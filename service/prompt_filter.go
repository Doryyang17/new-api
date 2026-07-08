package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	PromptFilterActionAllow = "allow"
	PromptFilterActionBlock = "block"
	PromptFilterActionWarn  = "warn"
	PromptFilterActionWatch = "monitor"

	defaultPromptFilterThreshold       = system_setting.DefaultPromptFilterThreshold
	defaultPromptFilterStrictThreshold = system_setting.DefaultPromptFilterStrictThreshold
	defaultPromptFilterMaxTextLength   = system_setting.DefaultPromptFilterMaxTextLength
	defaultPromptFilterHeadScanLength  = 64 * 1024
	defaultPromptFilterTailScanLength  = 16 * 1024
	promptFilterMatchTermMaxRunes      = 200
	promptFilterLogTextPreviewRunes    = 500
)

type PromptFilterMatch struct {
	Name     string `json:"name"`
	Weight   int    `json:"weight"`
	Category string `json:"category,omitempty"`
	Strict   bool   `json:"strict,omitempty"`
	Term     string `json:"term,omitempty"`
}

type PromptFilterVerdict struct {
	Enabled        bool                `json:"enabled"`
	Mode           string              `json:"mode"`
	Action         string              `json:"action"`
	Score          int                 `json:"score"`
	RawScore       int                 `json:"raw_score"`
	Threshold      int                 `json:"threshold"`
	StrictHit      bool                `json:"strict_hit"`
	Matched        []PromptFilterMatch `json:"matched"`
	Reason         string              `json:"reason,omitempty"`
	TextPreview    string              `json:"text_preview,omitempty"`
	FullText       string              `json:"full_text,omitempty"`
	ExtractedChars int                 `json:"extracted_chars"`
	Reviewed       bool                `json:"reviewed,omitempty"`
	ReviewFlagged  bool                `json:"review_flagged,omitempty"`
	ReviewError    string              `json:"review_error,omitempty"`
	ReviewModel    string              `json:"review_model,omitempty"`
}

type promptFilterPattern struct {
	name     string
	pattern  string
	weight   int
	category string
	strict   bool
	re       *regexp.Regexp
}

var (
	promptFilterPatternCacheMu    sync.RWMutex
	promptFilterPatternCachedKey  string
	promptFilterPatternCacheValue []promptFilterPattern

	promptFilterKeywordCacheMu    sync.RWMutex
	promptFilterKeywordCachedKey  string
	promptFilterKeywordCacheValue *promptFilterKeywordMatcher
)

var promptFilterPatternConfigs = []promptFilterPattern{
	{name: "credential_theft", pattern: `(?i)(?:^|[.!?。！？]\s*)(steal|dump|extract|exfiltrate|harvest|grab)\b.{0,50}\b(credentials?|passwords?|tokens?|cookies?)\b|\b(write|generate|create|give|build|craft|make|show|provide|implement|code|script|tool|steps?|instructions?|how\s+to|help\s+me|please)\b.{0,100}\b(steal|dump|extract|exfiltrate|harvest|grab)\b.{0,50}\b(credentials?|passwords?|tokens?|cookies?)\b|(?:写|生成|给我|构造|制作|提供|实现).{0,50}(窃取|导出|转储|提取).{0,30}(凭证|密码|令牌|token|cookie)`, weight: 100, category: "malicious", strict: true},
	{name: "malware_family", pattern: `(?i)\b(keylogger|ransomware|trojan|backdoor|botnet|infostealer)\b`, weight: 80, category: "malware", strict: true},
	{name: "evasion", pattern: `(?i)\b(bypass|disable|evade)\s+(av|edr|defender|antivirus|endpoint\s+detection)\b|免杀|绕过\s*(杀软|edr)`, weight: 80, category: "evasion", strict: true},
	{name: "persistence", pattern: `(?i)\b(persistence|persist(?:ent)?\s+access|startup\s+persistence|registry\s+run\s+key)\b`, weight: 35, category: "post_exploitation"},
	{name: "remote_shell", pattern: `(?i)\b(reverse\s+shell|bind\s+shell|meterpreter|web\s+shell)\b|反弹\s*shell`, weight: 45, category: "remote_access"},
	{name: "operational_remote_access_request", pattern: `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(reverse\s+shell|bind\s+shell|meterpreter|web\s+shell)\b|(?:写|生成|给我|构造|制作).{0,40}反弹\s*shell`, weight: 20, category: "remote_access"},
	{name: "exploit_payload", pattern: `(?i)\b(exploit\s+payload|payload\s+for\s+exploiting|weaponiz(?:e|ed|ation))\b`, weight: 45, category: "exploit"},
	{name: "operational_exploit_request", pattern: `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(exploit|payload|poc|proof[-\s]?of[-\s]?concept|0day|zero[-\s]?day)\b|(?:写|生成|给我|构造|制作).{0,40}(漏洞利用|攻击载荷|payload|poc)`, weight: 45, category: "exploit"},
	{name: "exploit_technique", pattern: `(?i)\b(shellcode|rop\s+chain|heap\s+spray|buffer\s+overflow\s+exploit)\b`, weight: 35, category: "exploit"},
	{name: "privilege_escalation", pattern: `(?i)\b(privilege\s+escalation|privesc|root\s+exploit|local\s+root)\b|提权`, weight: 35, category: "post_exploitation"},
	{name: "pentest_tooling", pattern: `(?i)\b(metasploit|cobalt\s+strike|mimikatz|empire|sliver\s+c2)\b`, weight: 30, category: "tooling"},
	{name: "scanner_tooling", pattern: `(?i)\b(sqlmap|nmap|masscan|zmap|burp\s+suite)\b`, weight: 15, category: "tooling"},
	{name: "large_scale_scanning", pattern: `(?i)\b(large[-\s]?scale|internet[-\s]?wide|public\s+ip\s+ranges?|mass)\s+(scan|scanning|enumeration)\b`, weight: 40, category: "scanning"},
	{name: "cve_reference", pattern: `(?i)\bcve-\d{4}-\d{4,7}\b`, weight: 10, category: "vulnerability"},
	{name: "generic_exploit", pattern: `(?i)\b(exploit|payload|vulnerability|0day|zero[-\s]?day)\b`, weight: 10, category: "vulnerability"},
	{name: "reverse_engineering", pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida\s+hook|deobfuscate|unpack)\b|反编译|脱壳`, weight: 15, category: "reverse_engineering"},
	{name: "reverse_engineering_secret_extraction", pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompile|disassembl|reverse\s+engineer)\b.{0,120}\b(extract|dump|recover|decrypt)\b.{0,80}\b(api\s*keys?|tokens?|secrets?|private\s*keys?|certificates?|license\s*keys?)\b|(?:ida|ghidra|frida|jadx|apktool|反编译|逆向).{0,80}(提取|导出|解密|恢复).{0,40}(密钥|token|令牌|私钥|证书|授权码)`, weight: 90, category: "reverse_engineering", strict: true},
	{name: "reverse_engineering_license_bypass", pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompile|disassembl|reverse\s+engineer)\b.{0,120}\b(bypass|crack|patch|remove|unlock)\b.{0,80}\b(license|activation|trial|paywall|subscription|in[-\s]?app\s+purchase|iap|entitlement)\b|(?:ida|ghidra|x64dbg|frida|反编译|逆向|脱壳|调试).{0,80}(绕过|破解|补丁|去除|解锁).{0,40}(授权|激活|试用|会员|订阅|付费|内购)`, weight: 85, category: "license_cracking", strict: true},
	{name: "reverse_engineering_anti_debug_bypass", pattern: `(?i)\b(bypass|disable|remove|defeat)\b.{0,60}\b(anti[-\s]?debug|anti[-\s]?tamper|integrity\s+check|root\s+detection|jailbreak\s+detection|certificate\s+pinning)\b|绕过.{0,40}(反调试|反篡改|完整性校验|root\s*检测|越狱检测|证书绑定|证书固定)`, weight: 70, category: "reverse_engineering", strict: true},
	{name: "frida_hook_abuse", pattern: `(?i)\b(frida|substrate|xposed)\b.{0,100}\b(hook|patch|bypass|unlock)\b.{0,80}\b(payment|purchase|license|activation|subscription|login|auth|entitlement)\b|(?:frida|xposed).{0,80}(hook|绕过|破解|解锁).{0,40}(支付|内购|授权|激活|会员|订阅|登录|鉴权)`, weight: 75, category: "reverse_engineering", strict: true},
	{name: "license_cracking", pattern: `(?i)\b(keygen|crack\s+license|serial\s+generator|license\s+bypass|patch\s+(activation|license))\b|注册机|破解授权|序列号生成`, weight: 55, category: "license_cracking", strict: true},
	{name: "data_exfiltration", pattern: `(?i)\b(exfiltrate|exfiltration|data\s+theft|steal\s+data|siphon\s+data)\b.{0,80}\b(database|files?|documents?|source\s+code|intellectual\s+property)\b|数据窃取|数据外泄`, weight: 70, category: "data_theft", strict: true},
	{name: "ddos_attack", pattern: `(?i)\b(ddos|dos\s+attack|distributed\s+denial|amplification\s+attack|syn\s+flood|udp\s+flood)\b|拒绝服务攻击|流量攻击`, weight: 65, category: "network_attack", strict: true},
	{name: "cryptomining_hijack", pattern: `(?i)\b(cryptojacking|coinhive|monero\s+miner|unauthorized\s+mining|hijack.{0,40}mining)\b|挖矿劫持|非法挖矿`, weight: 60, category: "resource_abuse", strict: true},
	{name: "phishing_social_engineering", pattern: `(?i)\b(phishing\s+(page|site|email)|credential\s+harvesting|fake\s+login|spoof\s+(domain|website))\b|钓鱼页面|伪造登录`, weight: 75, category: "social_engineering", strict: true},
	{name: "supply_chain_attack", pattern: `(?i)\b(supply\s+chain\s+attack|dependency\s+confusion|typosquatting|malicious\s+package|backdoor.{0,40}(npm|pypi|gem))\b|供应链攻击|依赖投毒`, weight: 70, category: "supply_chain", strict: true},
	{name: "container_escape", pattern: `(?i)\b(container\s+escape|docker\s+breakout|kubernetes\s+escape|privileged\s+container\s+exploit)\b|容器逃逸`, weight: 50, category: "container_security"},
	{name: "cloud_abuse", pattern: `(?i)\b(aws\s+key\s+leak|gcp\s+credential|azure\s+token|s3\s+bucket\s+takeover|iam\s+privilege\s+escalation)\b|云凭证泄露`, weight: 55, category: "cloud_security"},
	{name: "sql_injection_attack", pattern: `(?i)\b(sql\s+injection\s+payload|union\s+select\s+attack|blind\s+sqli|time[-\s]?based\s+sqli)\b|sql注入攻击`, weight: 40, category: "web_attack"},
	{name: "command_injection", pattern: `(?i)\b(command\s+injection|os\s+command\s+injection|shell\s+injection|rce\s+exploit)\b|命令注入`, weight: 50, category: "web_attack"},
	{name: "ssrf_xxe_attack", pattern: `(?i)\b(ssrf\s+exploit|server[-\s]?side\s+request\s+forgery|xxe\s+attack|xml\s+external\s+entity)\b`, weight: 35, category: "web_attack"},
	{name: "password_cracking", pattern: `(?i)\b(hashcat|john\s+the\s+ripper|password\s+cracking|brute[-\s]?force\s+(password|hash)|rainbow\s+table)\b|密码破解|暴力破解`, weight: 30, category: "credential_attack"},
	{name: "mitm_attack", pattern: `(?i)\b(man[-\s]?in[-\s]?the[-\s]?middle|mitm\s+attack|arp\s+spoofing|dns\s+spoofing|ssl\s+strip)\b|中间人攻击`, weight: 45, category: "network_attack"},
	{name: "wireless_attack", pattern: `(?i)\b(wpa2?\s+crack|wifi\s+deauth|evil\s+twin|rogue\s+access\s+point|aircrack)\b|wifi破解|无线攻击`, weight: 35, category: "wireless_attack"},
	{name: "firmware_iot_exploit", pattern: `(?i)\b(firmware\s+extraction|iot\s+exploit|router\s+backdoor|embedded\s+device\s+hack)\b|固件提取|物联网攻击`, weight: 40, category: "iot_security"},
	{name: "blockchain_exploit", pattern: `(?i)\b(smart\s+contract\s+exploit|reentrancy\s+attack|flash\s+loan\s+attack|private\s+key\s+theft)\b|智能合约漏洞|私钥窃取`, weight: 45, category: "blockchain_security"},
	{name: "session_hijacking", pattern: `(?i)\b(session\s+hijacking|cookie\s+theft|session\s+fixation|csrf\s+exploit)\b|会话劫持|cookie窃取`, weight: 40, category: "web_attack"},
	{name: "api_abuse", pattern: `(?i)\b(api\s+key\s+leak|rate\s+limit\s+bypass|api\s+abuse|unauthorized\s+api\s+access)\b|api密钥泄露|接口滥用`, weight: 35, category: "api_security"},
	{name: "steganography_covert", pattern: `(?i)\b(steganography|covert\s+channel|data\s+hiding|exfiltration\s+via\s+(dns|icmp))\b|隐写术|隐蔽信道`, weight: 30, category: "evasion"},
	{name: "ransomware_deployment", pattern: `(?i)\b(deploy\s+ransomware|ransomware\s+payload|encrypt\s+files\s+for\s+ransom|wannacry|locky)\b|部署勒索软件|加密勒索`, weight: 90, category: "malware", strict: true},
	{name: "botnet_c2", pattern: `(?i)\b(botnet\s+command|c2\s+server|command\s+and\s+control|zombie\s+network)\b|僵尸网络|c2服务器`, weight: 65, category: "malware", strict: true},
	{name: "xss_attack", pattern: `(?i)\b(xss\s+payload|cross[-\s]?site\s+scripting\s+attack|stored\s+xss|reflected\s+xss|dom\s+xss)\b|xss攻击载荷`, weight: 35, category: "web_attack"},
	{name: "deserialization_exploit", pattern: `(?i)\b(deserialization\s+exploit|insecure\s+deserialization|java\s+deserialization\s+attack|pickle\s+exploit)\b|反序列化漏洞`, weight: 45, category: "web_attack"},
	{name: "path_traversal", pattern: `(?i)\b(path\s+traversal|directory\s+traversal|\.\.\/|lfi\s+exploit|local\s+file\s+inclusion)\b|目录遍历|文件包含`, weight: 35, category: "web_attack"},
	{name: "memory_corruption", pattern: `(?i)\b(use[-\s]?after[-\s]?free|double\s+free|heap\s+overflow|stack\s+overflow\s+exploit)\b|内存破坏|堆溢出`, weight: 50, category: "exploit"},
	{name: "kernel_exploit", pattern: `(?i)\b(kernel\s+exploit|kernel\s+module\s+rootkit|dirty\s+cow|privilege\s+escalation\s+via\s+kernel)\b|内核漏洞|内核提权`, weight: 60, category: "exploit", strict: true},
	{name: "zero_click_exploit", pattern: `(?i)\b(zero[-\s]?click\s+exploit|remote\s+code\s+execution\s+without\s+interaction|wormable\s+exploit)\b|零点击漏洞`, weight: 70, category: "exploit", strict: true},
	{name: "sandbox_escape", pattern: `(?i)\b(sandbox\s+escape|vm\s+escape|browser\s+sandbox\s+bypass|jvm\s+sandbox\s+escape)\b|沙箱逃逸|虚拟机逃逸`, weight: 55, category: "exploit"},
	{name: "firmware_backdoor", pattern: `(?i)\b(firmware\s+backdoor|bios\s+rootkit|uefi\s+malware|bootkit)\b|固件后门|bios木马`, weight: 75, category: "malware", strict: true},
	{name: "supply_chain_backdoor", pattern: `(?i)\b(backdoor.{0,40}(npm|pypi|rubygems|maven)|trojanized\s+package|malicious\s+dependency)\b|依赖后门|恶意包`, weight: 70, category: "supply_chain", strict: true},
	{name: "credential_dumping", pattern: `(?i)\b(lsass\s+dump|sam\s+dump|ntds\.dit|credential\s+dumping|hashdump)\b|凭证转储|密码哈希导出`, weight: 65, category: "credential_attack", strict: true},
	{name: "lateral_movement", pattern: `(?i)\b(lateral\s+movement|pass[-\s]?the[-\s]?hash|pass[-\s]?the[-\s]?ticket|psexec|wmi\s+exec)\b|横向移动`, weight: 50, category: "post_exploitation"},
	{name: "domain_takeover", pattern: `(?i)\b(domain\s+takeover|subdomain\s+hijacking|dns\s+takeover|dangling\s+cname)\b|域名劫持|子域接管`, weight: 55, category: "network_attack"},
	{name: "token_theft", pattern: `(?i)\b(oauth\s+token\s+theft|jwt\s+hijacking|bearer\s+token\s+steal|access\s+token\s+exfiltration)\b|token窃取|令牌劫持`, weight: 60, category: "credential_attack", strict: true},
	{name: "process_injection", pattern: `(?i)\b(process\s+injection|dll\s+injection|reflective\s+loading|process\s+hollowing)\b|进程注入|dll注入`, weight: 55, category: "evasion"},
	{name: "fileless_malware", pattern: `(?i)\b(fileless\s+malware|living\s+off\s+the\s+land|lolbins|powershell\s+empire)\b|无文件攻击`, weight: 50, category: "evasion"},
	{name: "log_tampering", pattern: `(?i)\b(log\s+deletion|event\s+log\s+clearing|anti[-\s]?forensics|cover\s+tracks)\b|日志清除|反取证`, weight: 45, category: "evasion"},
	{name: "vpn_proxy_abuse", pattern: `(?i)\b(vpn\s+exploit|proxy\s+chain\s+for\s+anonymity|tor\s+hidden\s+service\s+setup)\b|vpn漏洞|代理链匿名`, weight: 30, category: "evasion"},
	{name: "database_dump", pattern: `(?i)\b(database\s+dump|mysqldump\s+attack|mongodb\s+ransom|elasticsearch\s+exposure)\b|数据库导出|数据库勒索`, weight: 55, category: "data_theft"},
	{name: "api_key_scraping", pattern: `(?i)\b(scrape\s+api\s+keys|github\s+secret\s+scanning|hardcoded\s+credentials\s+search)\b|api密钥爬取|硬编码凭证搜索`, weight: 50, category: "credential_attack"},
	{name: "mass_exploitation", pattern: `(?i)\b(mass\s+exploitation|automated\s+exploitation|exploit\s+at\s+scale|worm\s+propagation)\b|大规模利用|蠕虫传播`, weight: 70, category: "exploit", strict: true},
	{name: "insider_threat", pattern: `(?i)\b(insider\s+threat|rogue\s+employee|data\s+theft\s+by\s+employee|sabotage)\b|内部威胁|员工窃密`, weight: 40, category: "data_theft"},
	{name: "cryptographic_attack", pattern: `(?i)\b(padding\s+oracle|timing\s+attack|side[-\s]?channel\s+attack|weak\s+encryption\s+exploit)\b|密码学攻击|侧信道攻击`, weight: 35, category: "crypto_attack"},
	{name: "race_condition_exploit", pattern: `(?i)\b(race\s+condition\s+exploit|toctou|time[-\s]?of[-\s]?check\s+time[-\s]?of[-\s]?use)\b|竞态条件漏洞`, weight: 30, category: "exploit"},
	{name: "hardware_implant", pattern: `(?i)\b(hardware\s+implant|usb\s+rubber\s+ducky|malicious\s+usb|hardware\s+keylogger)\b|硬件植入|恶意usb`, weight: 60, category: "physical_attack", strict: true},
	{name: "social_media_hijack", pattern: `(?i)\b(account\s+takeover|social\s+media\s+hijacking|credential\s+stuffing)\b|账号接管|撞库攻击`, weight: 40, category: "credential_attack"},
}

var promptFilterDefensiveContextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(defensive|defense|prevent|prevention|mitigation|detect|detection|hardening|patch|remediation|incident\s+response)\b`),
	regexp.MustCompile(`(?i)\b(do\s+not\s+provide|without\s+code|no\s+commands|high\s+level|non[-\s]?operational|refusal|unsafe)\b`),
	regexp.MustCompile(`防御|修复|检测|加固|不要提供|不提供代码`),
}

func InspectPromptBody(body []byte, endpoint string) PromptFilterVerdict {
	return InspectPromptBodyWithContext(context.Background(), body, endpoint)
}

func InspectPromptBodyWithContext(ctx context.Context, body []byte, endpoint string) PromptFilterVerdict {
	return InspectPromptRequestBodyWithContext(ctx, body, "", endpoint)
}

func InspectPromptRequestBodyWithContext(ctx context.Context, body []byte, contentType string, endpoint string) PromptFilterVerdict {
	settings := system_setting.GetPromptFilterSettings()
	text := ExtractPromptTextFromRequestBody(body, contentType, endpoint, settings.MaxTextLength)
	return inspectPromptText(ctx, text, false)
}

func InspectPromptRequestReadSeekerWithContext(ctx context.Context, body io.ReadSeeker, size int64, contentType string, endpoint string) (PromptFilterVerdict, error) {
	if body == nil {
		return inspectPromptText(ctx, "", false), nil
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return PromptFilterVerdict{}, err
	}
	settings := system_setting.GetPromptFilterSettings()
	text, err := ExtractPromptTextFromRequestReadSeeker(body, size, contentType, endpoint, settings.MaxTextLength)
	_, resetErr := body.Seek(0, io.SeekStart)
	if err != nil {
		return PromptFilterVerdict{}, err
	}
	if resetErr != nil {
		return PromptFilterVerdict{}, resetErr
	}
	return inspectPromptText(ctx, text, false), nil
}

func InspectPromptText(text string) PromptFilterVerdict {
	return inspectPromptText(context.Background(), text, false)
}

func InspectPromptTextForTest(text string) PromptFilterVerdict {
	return inspectPromptText(context.Background(), text, true)
}

func PromptFilterRequestWhitelisted(c *gin.Context, settings system_setting.PromptFilterSettings) bool {
	if c == nil {
		return false
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	for _, allowedGroup := range settings.GroupWhitelist {
		if allowedGroup == group {
			return true
		}
	}
	channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if channelId == 0 {
		channelId = c.GetInt("channel_id")
	}
	for _, allowedChannelId := range settings.ChannelWhitelist {
		if allowedChannelId == channelId {
			return true
		}
	}
	return false
}

func inspectPromptText(ctx context.Context, text string, forceEnabled bool) PromptFilterVerdict {
	settings := system_setting.GetPromptFilterSettings()
	verdict := PromptFilterVerdict{
		Enabled:        forceEnabled || setting.ShouldCheckPromptSensitive(),
		Mode:           settings.Mode,
		Action:         PromptFilterActionAllow,
		Threshold:      settings.Threshold,
		TextPreview:    promptFilterPreview(text, 500),
		FullText:       text,
		ExtractedChars: utf8.RuneCountInString(text),
	}
	if !verdict.Enabled || strings.TrimSpace(text) == "" {
		return verdict
	}

	patterns, err := getPromptFilterPatterns(settings)
	if err != nil {
		verdict.Reason = err.Error()
		return verdict
	}

	scanText := promptFilterNormalizeForScan(promptFilterLimitScanText(text, settings.MaxTextLength))
	if utf8.RuneCountInString(scanText) < 3 {
		return verdict
	}

	matchesByName := map[string]PromptFilterMatch{}
	for key, match := range getPromptFilterKeywordMatcher(settings).MatchesByKey(scanText) {
		matchesByName[key] = match
	}
	for _, pattern := range patterns {
		if term, ok := promptFilterPatternMatchTerm(pattern.re, scanText); ok {
			matchesByName[pattern.name] = PromptFilterMatch{
				Name:     pattern.name,
				Weight:   pattern.weight,
				Category: pattern.category,
				Strict:   pattern.strict,
				Term:     term,
			}
		}
	}

	matches := make([]PromptFilterMatch, 0, len(matchesByName))
	rawScore := 0
	strictScore := 0
	for _, match := range matchesByName {
		matches = append(matches, match)
		rawScore += match.Weight
		if match.Strict {
			strictScore += match.Weight
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Weight == matches[j].Weight {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].Weight > matches[j].Weight
	})

	score := rawScore
	if rawScore > 0 {
		contextDiscount := promptFilterDefensiveContextDiscount(scanText)
		score -= contextDiscount
		if score < 0 {
			score = 0
		}
	}
	strictHit := strictScore >= settings.StrictThreshold
	if score >= settings.Threshold || strictHit {
		switch settings.Mode {
		case system_setting.PromptFilterModeWarn:
			verdict.Action = PromptFilterActionWarn
		case system_setting.PromptFilterModeMonitor:
			verdict.Action = PromptFilterActionWatch
		default:
			verdict.Action = PromptFilterActionBlock
		}
	}

	verdict.Score = score
	verdict.RawScore = rawScore
	verdict.StrictHit = strictHit
	verdict.Matched = matches
	if len(matches) > 0 {
		verdict.Reason = promptFilterReason(verdict.Action, score, settings.Threshold, matches)
	}
	if shouldReviewPromptFilterVerdict(verdict, settings) {
		verdict = reviewPromptFilterVerdict(ctx, text, verdict, settings)
	}
	return verdict
}

func ExtractPromptText(body []byte, endpoint string, maxLen int) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	parts := make([]string, 0, 8)
	add := func(result gjson.Result) {
		if result.Exists() {
			collectPromptGJSONText(result, &parts)
		}
	}

	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	switch endpoint {
	case "chat", "chat_completions", "/v1/chat/completions", "/v1/completions", "/v1/moderations":
		add(gjson.GetBytes(body, "messages"))
		add(gjson.GetBytes(body, "prompt"))
		add(gjson.GetBytes(body, "input"))
	case "messages", "claude", "/v1/messages":
		add(gjson.GetBytes(body, "system"))
		add(gjson.GetBytes(body, "messages"))
	case "responses", "openai_responses", "openai_responses_compaction", "/v1/responses", "/v1/responses/compact":
		add(gjson.GetBytes(body, "instructions"))
		add(gjson.GetBytes(body, "input"))
		add(gjson.GetBytes(body, "prompt"))
	case "image", "images", "images_generations", "images_edits", "/v1/images/generations", "/v1/images/edits", "/v1/edits":
		add(gjson.GetBytes(body, "prompt"))
		add(gjson.GetBytes(body, "style"))
	case "gemini", "/v1beta/models":
		add(gjson.GetBytes(body, "systemInstruction"))
		add(gjson.GetBytes(body, "system_instruction"))
		add(gjson.GetBytes(body, "contents"))
		add(gjson.GetBytes(body, "requests"))
	case "realtime", "/v1/realtime":
		add(gjson.GetBytes(body, "session.instructions"))
		add(gjson.GetBytes(body, "session.tools"))
		add(gjson.GetBytes(body, "item.name"))
		add(gjson.GetBytes(body, "item.content"))
		add(gjson.GetBytes(body, "response.instructions"))
	case "task", "midjourney", "video", "suno":
		add(gjson.GetBytes(body, "prompt"))
		add(gjson.GetBytes(body, "content"))
		add(gjson.GetBytes(body, "metadata.prompt"))
		add(gjson.GetBytes(body, "metadata.content"))
	default:
		add(gjson.GetBytes(body, "instructions"))
		add(gjson.GetBytes(body, "input"))
		add(gjson.GetBytes(body, "prompt"))
		add(gjson.GetBytes(body, "messages"))
		add(gjson.GetBytes(body, "system"))
		add(gjson.GetBytes(body, "contents"))
		add(gjson.GetBytes(body, "content"))
		add(gjson.GetBytes(body, "metadata.prompt"))
	}
	return promptFilterLimitScanText(strings.Join(parts, "\n"), maxLen)
}

func ExtractPromptTextFromRequestBody(body []byte, contentType string, endpoint string, maxLen int) string {
	if len(body) == 0 {
		return ""
	}
	if text := extractPromptTextFromFormBody(body, contentType, maxLen); text != "" {
		return text
	}
	return ExtractPromptText(body, endpoint, maxLen)
}

func ExtractPromptTextFromRequestReadSeeker(body io.ReadSeeker, size int64, contentType string, endpoint string, maxLen int) (string, error) {
	if body == nil {
		return "", nil
	}
	if text, handled, err := extractPromptTextFromFormReader(body, contentType, maxLen); handled {
		return text, err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if text, err := extractPromptTextFromJSONReader(body, endpoint, maxLen); err == nil && text != "" {
		return text, nil
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	data, err := readPromptFilterBoundedReadSeeker(body, size, promptFilterBodyReadLimit(maxLen))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	if text := ExtractPromptText(data, endpoint, maxLen); text != "" {
		return text, nil
	}
	return promptFilterLimitScanText(string(data), maxLen), nil
}

func extractPromptTextFromJSONReader(reader io.Reader, endpoint string, maxLen int) (string, error) {
	scanner := newPromptFilterJSONScanner(reader, endpoint, maxLen)
	if err := scanner.Scan(); err != nil {
		return "", err
	}
	return scanner.Text(), nil
}

type promptFilterJSONScanner struct {
	reader    *bufio.Reader
	endpoint  string
	collector *promptFilterTextCollector
	maxDepth  int
}

func newPromptFilterJSONScanner(reader io.Reader, endpoint string, maxLen int) *promptFilterJSONScanner {
	return &promptFilterJSONScanner{
		reader:    bufio.NewReaderSize(reader, 32*1024),
		endpoint:  endpoint,
		collector: newPromptFilterTextCollector(maxLen),
		maxDepth:  128,
	}
}

func (s *promptFilterJSONScanner) Scan() error {
	first, err := s.nextNonSpace()
	if err != nil {
		return err
	}
	return s.scanValue(first, nil, 0)
}

func (s *promptFilterJSONScanner) Text() string {
	return s.collector.String()
}

func (s *promptFilterJSONScanner) scanValue(first byte, path []string, depth int) error {
	if depth > s.maxDepth {
		return fmt.Errorf("json depth exceeds %d", s.maxDepth)
	}
	switch first {
	case '{':
		return s.scanObject(path, depth+1)
	case '[':
		return s.scanArray(path, depth+1)
	case '"':
		if promptFilterShouldCollectJSONStringPath(s.endpoint, path) {
			return s.collectJSONStringValue()
		}
		return s.skipJSONString()
	default:
		return s.skipJSONPrimitive(first)
	}
}

func (s *promptFilterJSONScanner) scanObject(path []string, depth int) error {
	for {
		next, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		if next == '}' {
			return nil
		}
		if next != '"' {
			return fmt.Errorf("expected json object key")
		}
		key, err := s.readJSONStringKey()
		if err != nil {
			return err
		}
		if err := s.expectJSONByte(':'); err != nil {
			return err
		}
		valueFirst, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			if err := s.skipJSONValue(valueFirst, depth+1); err != nil {
				return err
			}
		} else if promptFilterJSONSkipField(normalizedKey) {
			if err := s.skipJSONValue(valueFirst, depth+1); err != nil {
				return err
			}
		} else {
			if err := s.scanValue(valueFirst, append(path, normalizedKey), depth+1); err != nil {
				return err
			}
		}
		delimiter, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		switch delimiter {
		case ',':
			continue
		case '}':
			return nil
		default:
			return fmt.Errorf("expected json object delimiter")
		}
	}
}

func (s *promptFilterJSONScanner) scanArray(path []string, depth int) error {
	for {
		next, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		if next == ']' {
			return nil
		}
		if err := s.scanValue(next, path, depth+1); err != nil {
			return err
		}
		delimiter, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		switch delimiter {
		case ',':
			continue
		case ']':
			return nil
		default:
			return fmt.Errorf("expected json array delimiter")
		}
	}
}

func (s *promptFilterJSONScanner) skipJSONValue(first byte, depth int) error {
	if depth > s.maxDepth {
		return fmt.Errorf("json depth exceeds %d", s.maxDepth)
	}
	switch first {
	case '{':
		return s.skipJSONObject(depth + 1)
	case '[':
		return s.skipJSONArray(depth + 1)
	case '"':
		return s.skipJSONString()
	default:
		return s.skipJSONPrimitive(first)
	}
}

func (s *promptFilterJSONScanner) skipJSONObject(depth int) error {
	for {
		next, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		if next == '}' {
			return nil
		}
		if next != '"' {
			return fmt.Errorf("expected json object key")
		}
		if err := s.skipJSONString(); err != nil {
			return err
		}
		if err := s.expectJSONByte(':'); err != nil {
			return err
		}
		valueFirst, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		if err := s.skipJSONValue(valueFirst, depth+1); err != nil {
			return err
		}
		delimiter, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		switch delimiter {
		case ',':
			continue
		case '}':
			return nil
		default:
			return fmt.Errorf("expected json object delimiter")
		}
	}
}

func (s *promptFilterJSONScanner) skipJSONArray(depth int) error {
	for {
		next, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		if next == ']' {
			return nil
		}
		if err := s.skipJSONValue(next, depth+1); err != nil {
			return err
		}
		delimiter, err := s.nextNonSpace()
		if err != nil {
			return err
		}
		switch delimiter {
		case ',':
			continue
		case ']':
			return nil
		default:
			return fmt.Errorf("expected json array delimiter")
		}
	}
}

func (s *promptFilterJSONScanner) collectJSONStringValue() error {
	s.collector.StartValue()
	chunk := make([]byte, 0, 4096)
	flush := func() {
		if len(chunk) > 0 {
			s.collector.AddRaw(string(chunk))
			chunk = chunk[:0]
		}
	}
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return err
		}
		switch b {
		case '"':
			flush()
			return nil
		case '\\':
			decoded, err := s.readJSONEscape()
			if err != nil {
				return err
			}
			chunk = append(chunk, decoded...)
		default:
			chunk = append(chunk, b)
		}
		if len(chunk) >= cap(chunk) {
			flush()
		}
	}
}

func (s *promptFilterJSONScanner) readJSONStringKey() (string, error) {
	var builder strings.Builder
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return "", err
		}
		switch b {
		case '"':
			return builder.String(), nil
		case '\\':
			decoded, err := s.readJSONEscape()
			if err != nil {
				return "", err
			}
			builder.Write(decoded)
		default:
			builder.WriteByte(b)
		}
	}
}

func (s *promptFilterJSONScanner) skipJSONString() error {
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return err
		}
		switch b {
		case '"':
			return nil
		case '\\':
			if _, err := s.reader.ReadByte(); err != nil {
				return err
			}
		}
	}
}

func (s *promptFilterJSONScanner) readJSONEscape() ([]byte, error) {
	escaped, err := s.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch escaped {
	case '"', '\\', '/':
		return []byte{escaped}, nil
	case 'b':
		return []byte{'\b'}, nil
	case 'f':
		return []byte{'\f'}, nil
	case 'n':
		return []byte{'\n'}, nil
	case 'r':
		return []byte{'\r'}, nil
	case 't':
		return []byte{'\t'}, nil
	case 'u':
		r, err := s.readJSONUnicodeEscape()
		if err != nil {
			return nil, err
		}
		return []byte(string(r)), nil
	default:
		return nil, fmt.Errorf("invalid json escape")
	}
}

func (s *promptFilterJSONScanner) readJSONUnicodeEscape() (rune, error) {
	var value rune
	for i := 0; i < 4; i++ {
		b, err := s.reader.ReadByte()
		if err != nil {
			return utf8.RuneError, err
		}
		digit, ok := promptFilterJSONHexValue(b)
		if !ok {
			return utf8.RuneError, fmt.Errorf("invalid json unicode escape")
		}
		value = value*16 + rune(digit)
	}
	return value, nil
}

func promptFilterJSONHexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func (s *promptFilterJSONScanner) skipJSONPrimitive(first byte) error {
	if promptFilterJSONPrimitiveDelimiter(first) {
		return s.reader.UnreadByte()
	}
	for {
		b, err := s.reader.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if promptFilterJSONPrimitiveDelimiter(b) {
			return s.reader.UnreadByte()
		}
	}
}

func promptFilterJSONPrimitiveDelimiter(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t', ',', '}', ']':
		return true
	default:
		return false
	}
}

func (s *promptFilterJSONScanner) expectJSONByte(expected byte) error {
	got, err := s.nextNonSpace()
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("expected json byte %q", expected)
	}
	return nil
}

func (s *promptFilterJSONScanner) nextNonSpace() (byte, error) {
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return b, nil
		}
	}
}

func extractPromptTextFromFormBody(body []byte, contentType string, maxLen int) string {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	switch strings.ToLower(mediaType) {
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return ""
		}
		return promptFilterTextFromValues(values, maxLen)
	case "multipart/form-data":
		boundary := strings.TrimSpace(params["boundary"])
		if boundary == "" {
			return ""
		}
		return promptFilterTextFromMultipart(body, boundary, maxLen)
	default:
		return ""
	}
}

func extractPromptTextFromFormReader(reader io.Reader, contentType string, maxLen int) (string, bool, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", false, nil
	}
	switch strings.ToLower(mediaType) {
	case "application/x-www-form-urlencoded":
		data, err := readPromptFilterLimited(reader, promptFilterBodyReadLimit(maxLen))
		if err != nil {
			return "", true, err
		}
		values, err := url.ParseQuery(string(data))
		if err != nil {
			return "", true, nil
		}
		return promptFilterTextFromValues(values, maxLen), true, nil
	case "multipart/form-data":
		boundary := strings.TrimSpace(params["boundary"])
		if boundary == "" {
			return "", true, nil
		}
		text, err := promptFilterTextFromMultipartReader(reader, boundary, maxLen)
		return text, true, err
	default:
		return "", false, nil
	}
}

func promptFilterTextFromValues(values url.Values, maxLen int) string {
	parts := make([]string, 0, len(values))
	for name, fieldValues := range values {
		if !promptFilterFormTextField(name) {
			continue
		}
		for _, value := range fieldValues {
			if text := strings.TrimSpace(value); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return promptFilterLimitScanText(strings.Join(parts, "\n"), maxLen)
}

func promptFilterTextFromMultipartReader(reader io.Reader, boundary string, maxLen int) (string, error) {
	multipartReader := multipart.NewReader(reader, boundary)
	parts := make([]string, 0, 4)
	for {
		part, err := multipartReader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if part.FileName() != "" || !promptFilterFormTextField(part.FormName()) {
			_ = part.Close()
			continue
		}
		limited, err := io.ReadAll(io.LimitReader(part, int64(normalizedPromptFilterMaxTextLength(maxLen))+1))
		if closeErr := part.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return "", err
		}
		if text := strings.TrimSpace(string(limited)); text != "" {
			parts = append(parts, text)
		}
	}
	return promptFilterLimitScanText(strings.Join(parts, "\n"), maxLen), nil
}

func promptFilterTextFromMultipart(body []byte, boundary string, maxLen int) string {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	parts := make([]string, 0, 4)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ""
		}
		if part.FileName() != "" || !promptFilterFormTextField(part.FormName()) {
			continue
		}
		limited, err := io.ReadAll(io.LimitReader(part, int64(normalizedPromptFilterMaxTextLength(maxLen))+1))
		if err != nil {
			return ""
		}
		if text := strings.TrimSpace(string(limited)); text != "" {
			parts = append(parts, text)
		}
	}
	return promptFilterLimitScanText(strings.Join(parts, "\n"), maxLen)
}

func readPromptFilterLimited(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = int64(defaultPromptFilterMaxTextLength)
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return data[:limit], nil
	}
	return data, nil
}

func readPromptFilterBoundedReadSeeker(reader io.ReadSeeker, size int64, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = int64(defaultPromptFilterMaxTextLength)
	}
	if size <= 0 || size <= limit {
		return readPromptFilterLimited(reader, limit)
	}

	tailSize := int64(defaultPromptFilterTailScanLength)
	if tailSize > limit/4 {
		tailSize = limit / 4
	}
	headSize := limit - tailSize - 1
	if headSize < 1 {
		headSize = limit
		tailSize = 0
	}

	head := make([]byte, headSize)
	headRead, err := io.ReadFull(reader, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	head = head[:headRead]

	if tailSize <= 0 {
		return head, nil
	}
	if _, err := reader.Seek(size-tailSize, io.SeekStart); err != nil {
		return nil, err
	}
	tail := make([]byte, tailSize)
	tailRead, err := io.ReadFull(reader, tail)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	tail = tail[:tailRead]

	data := make([]byte, 0, len(head)+1+len(tail))
	data = append(data, head...)
	data = append(data, '\n')
	data = append(data, tail...)
	return data, nil
}

func promptFilterBodyReadLimit(maxLen int) int64 {
	limit := int64(normalizedPromptFilterMaxTextLength(maxLen))*4 + 256*1024
	const minLimit = int64(256 * 1024)
	const maxLimit = int64(4 * 1024 * 1024)
	if limit < minLimit {
		return minLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func promptFilterFormTextField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "prompt", "negative_prompt", "input", "content", "message", "messages", "instructions", "system", "text", "metadata", "metadata.prompt", "metadata.content":
		return true
	default:
		return false
	}
}

func promptFilterShouldCollectJSONStringPath(endpoint string, path []string) bool {
	if len(path) == 0 {
		return false
	}
	normalizedPath := make([]string, 0, len(path))
	for _, item := range path {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if promptFilterJSONSkipField(item) {
			return false
		}
		normalizedPath = append(normalizedPath, item)
	}
	if len(normalizedPath) == 0 {
		return false
	}
	for _, prefix := range promptFilterJSONPromptPrefixes(endpoint) {
		if promptFilterJSONPathHasPrefix(normalizedPath, prefix) {
			return true
		}
	}
	return false
}

func promptFilterJSONPromptPrefixes(endpoint string) [][]string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "chat", "chat_completions", "/v1/chat/completions", "/v1/completions", "/v1/moderations":
		return [][]string{{"messages"}, {"prompt"}, {"input"}}
	case "messages", "claude", "/v1/messages":
		return [][]string{{"system"}, {"messages"}}
	case "responses", "openai_responses", "openai_responses_compaction", "/v1/responses", "/v1/responses/compact":
		return [][]string{{"instructions"}, {"input"}, {"prompt"}}
	case "image", "images", "images_generations", "images_edits", "/v1/images/generations", "/v1/images/edits", "/v1/edits":
		return [][]string{{"prompt"}, {"style"}}
	case "gemini", "/v1beta/models":
		return [][]string{{"systeminstruction"}, {"system_instruction"}, {"contents"}, {"requests"}}
	case "realtime", "/v1/realtime":
		return [][]string{{"session", "instructions"}, {"session", "tools"}, {"item", "name"}, {"item", "content"}, {"response", "instructions"}}
	case "task", "midjourney", "video", "suno":
		return [][]string{{"prompt"}, {"content"}, {"metadata", "prompt"}, {"metadata", "content"}}
	default:
		return [][]string{{"instructions"}, {"input"}, {"prompt"}, {"messages"}, {"system"}, {"contents"}, {"content"}, {"metadata", "prompt"}}
	}
}

func promptFilterJSONPathHasPrefix(path []string, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i, item := range prefix {
		if path[i] != item {
			return false
		}
	}
	return true
}

func promptFilterJSONSkipField(field string) bool {
	switch field {
	case "image_url", "url", "file_id", "result", "data", "b64_json", "source", "file", "type", "role", "inline_data", "inlinedata", "mime_type", "mimetype", "audio", "video_url", "input_audio", "image", "images", "mask", "base64", "base64array", "sourcebase64", "targetbase64", "maskbase64":
		return true
	default:
		return false
	}
}

type promptFilterTextCollector struct {
	maxLen    int
	headLimit int
	tailLimit int
	total     int
	wrote     bool
	truncated bool
	full      strings.Builder
	head      strings.Builder
	tail      []byte
}

func newPromptFilterTextCollector(maxLen int) *promptFilterTextCollector {
	if maxLen <= 0 {
		maxLen = defaultPromptFilterMaxTextLength
	}
	headLimit := defaultPromptFilterHeadScanLength
	tailLimit := defaultPromptFilterTailScanLength
	if maxLen < headLimit+tailLimit {
		headLimit = maxLen * 4 / 5
		tailLimit = maxLen - headLimit
	}
	return &promptFilterTextCollector{
		maxLen:    maxLen,
		headLimit: headLimit,
		tailLimit: tailLimit,
	}
}

func (c *promptFilterTextCollector) Add(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	c.StartValue()
	c.addRaw(text)
}

func (c *promptFilterTextCollector) StartValue() {
	if c.wrote {
		c.addRaw("\n")
		return
	}
	c.wrote = true
}

func (c *promptFilterTextCollector) AddRaw(text string) {
	c.addRaw(text)
}

func (c *promptFilterTextCollector) addRaw(text string) {
	if text == "" {
		return
	}
	if !c.truncated && c.total+len(text) <= c.maxLen {
		c.full.WriteString(text)
		c.total += len(text)
		return
	}
	if !c.truncated {
		existing := c.full.String()
		c.full.Reset()
		c.truncated = true
		c.appendHead(existing)
		c.appendHead(text)
		c.appendTail(existing)
		c.appendTail(text)
		c.total += len(text)
		return
	}
	c.appendTail(text)
	c.total += len(text)
}

func (c *promptFilterTextCollector) appendHead(text string) {
	if text == "" || c.headLimit <= 0 {
		return
	}
	remaining := c.headLimit - c.head.Len()
	if remaining <= 0 {
		return
	}
	c.head.WriteString(safePromptFilterUTF8Prefix(text, remaining))
}

func (c *promptFilterTextCollector) appendTail(text string) {
	if text == "" || c.tailLimit <= 0 {
		return
	}
	if len(text) >= c.tailLimit {
		c.tail = []byte(safePromptFilterUTF8Suffix(text, c.tailLimit))
		return
	}
	c.tail = append(c.tail, text...)
	if len(c.tail) > c.tailLimit {
		c.tail = c.tail[len(c.tail)-c.tailLimit:]
		for len(c.tail) > 0 && !utf8.RuneStart(c.tail[0]) {
			c.tail = c.tail[1:]
		}
	}
}

func (c *promptFilterTextCollector) String() string {
	if !c.truncated {
		return c.full.String()
	}
	head := safePromptFilterUTF8Prefix(c.head.String(), c.headLimit)
	tail := safePromptFilterUTF8Suffix(string(c.tail), c.tailLimit)
	if head == "" {
		return tail
	}
	if tail == "" {
		return head
	}
	return head + "\n" + tail
}

func normalizedPromptFilterMaxTextLength(maxLen int) int {
	if maxLen <= 0 {
		return defaultPromptFilterMaxTextLength
	}
	return maxLen
}

func getPromptFilterPatterns(settings system_setting.PromptFilterSettings) ([]promptFilterPattern, error) {
	cacheKey := promptFilterPatternCacheKey(settings)
	promptFilterPatternCacheMu.RLock()
	if promptFilterPatternCachedKey == cacheKey && promptFilterPatternCacheValue != nil {
		cached := promptFilterPatternCacheValue
		promptFilterPatternCacheMu.RUnlock()
		return cached, nil
	}
	promptFilterPatternCacheMu.RUnlock()

	disabledPatterns := make(map[string]struct{}, len(settings.DisabledPatterns))
	for _, name := range settings.DisabledPatterns {
		disabledPatterns[name] = struct{}{}
	}

	patterns := make([]promptFilterPattern, 0, len(promptFilterPatternConfigs)+len(settings.CustomPatterns))
	for _, pattern := range promptFilterPatternConfigs {
		if _, disabled := disabledPatterns[pattern.name]; disabled {
			continue
		}
		patterns = append(patterns, pattern)
	}
	for _, pattern := range settings.CustomPatterns {
		if pattern.Enabled != nil && !*pattern.Enabled {
			continue
		}
		if _, disabled := disabledPatterns[pattern.Name]; disabled {
			continue
		}
		patterns = append(patterns, promptFilterPattern{
			name:     pattern.Name,
			pattern:  pattern.Pattern,
			weight:   pattern.Weight,
			category: pattern.Category,
			strict:   pattern.Strict,
		})
	}

	compiledPatterns := make([]promptFilterPattern, 0, len(patterns))
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern.pattern)
		if err != nil {
			return nil, fmt.Errorf("compile prompt filter pattern %q: %w", pattern.name, err)
		}
		pattern.re = compiled
		compiledPatterns = append(compiledPatterns, pattern)
	}
	promptFilterPatternCacheMu.Lock()
	promptFilterPatternCachedKey = cacheKey
	promptFilterPatternCacheValue = compiledPatterns
	promptFilterPatternCacheMu.Unlock()
	return compiledPatterns, nil
}

func promptFilterPatternCacheKey(settings system_setting.PromptFilterSettings) string {
	type cacheKey struct {
		CustomPatterns   []system_setting.PromptFilterCustomPattern `json:"custom_patterns"`
		DisabledPatterns []string                                   `json:"disabled_patterns"`
	}
	payload := cacheKey{
		CustomPatterns:   settings.CustomPatterns,
		DisabledPatterns: settings.DisabledPatterns,
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v:%v", settings.CustomPatterns, settings.DisabledPatterns)
	}
	return string(data)
}

func collectPromptGJSONText(result gjson.Result, parts *[]string) {
	if !result.Exists() || result.Type == gjson.Null {
		return
	}
	switch {
	case result.IsArray():
		for _, item := range result.Array() {
			collectPromptGJSONText(item, parts)
		}
	case result.IsObject():
		if textValue := result.Get("text"); textValue.Type == gjson.String {
			if text := strings.TrimSpace(textValue.String()); text != "" {
				*parts = append(*parts, text)
			}
		}
		if contentValue := result.Get("content"); contentValue.Exists() {
			if contentValue.Type == gjson.String {
				if text := strings.TrimSpace(contentValue.String()); text != "" {
					*parts = append(*parts, text)
				}
			} else {
				collectPromptGJSONText(contentValue, parts)
			}
		}
		result.ForEach(func(key, value gjson.Result) bool {
			switch strings.ToLower(key.String()) {
			case "text", "content", "image_url", "url", "file_id", "result", "data", "b64_json", "source", "file", "type", "role", "inline_data", "inlinedata", "mime_type", "mimetype", "audio", "video_url", "input_audio", "image", "images", "mask", "base64array", "sourcebase64", "targetbase64", "maskbase64":
				return true
			}
			collectPromptGJSONText(value, parts)
			return true
		})
	case result.Type == gjson.String:
		if text := strings.TrimSpace(result.String()); text != "" {
			*parts = append(*parts, text)
		}
	}
}

func normalizedPromptFilterSensitiveWords() []string {
	seen := map[string]struct{}{}
	words := make([]string, 0, len(setting.SensitiveWords))
	for _, word := range setting.SensitiveWords {
		word = promptFilterNormalizeForScan(strings.TrimSpace(word))
		if word == "" {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		words = append(words, word)
	}
	return words
}

func promptFilterNormalizeForScan(text string) string {
	text = strings.ReplaceAll(text, "```", " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ' '
		}
		return unicode.ToLower(r)
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

func promptFilterLimitScanText(text string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultPromptFilterMaxTextLength
	}
	if len(text) <= maxLen {
		return text
	}
	head := defaultPromptFilterHeadScanLength
	tail := defaultPromptFilterTailScanLength
	if maxLen < head+tail {
		head = maxLen * 4 / 5
		tail = maxLen - head
	}
	if head > len(text) {
		head = len(text)
	}
	if tail > len(text)-head {
		tail = len(text) - head
	}
	return safePromptFilterUTF8Prefix(text, head) + "\n" + safePromptFilterUTF8Suffix(text, tail)
}

func safePromptFilterUTF8Prefix(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes >= len(text) {
		return text
	}
	for maxBytes > 0 && !utf8.ValidString(text[:maxBytes]) {
		maxBytes--
	}
	return text[:maxBytes]
}

func safePromptFilterUTF8Suffix(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes >= len(text) {
		return text
	}
	start := len(text) - maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return text[start:]
}

func promptFilterPreview(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func promptFilterDefensiveContextDiscount(text string) int {
	discount := 0
	for _, pattern := range promptFilterDefensiveContextPatterns {
		if pattern.MatchString(text) {
			discount += 30
		}
	}
	if discount > 90 {
		return 90
	}
	return discount
}

func promptFilterReason(action string, score int, threshold int, matches []PromptFilterMatch) string {
	if len(matches) == 0 {
		return ""
	}
	names := make([]string, 0, len(matches))
	for i, match := range matches {
		if i >= 3 {
			break
		}
		names = append(names, match.Name)
	}
	if action == PromptFilterActionBlock {
		return fmt.Sprintf("prompt blocked: score %d >= %d (%s)", score, threshold, strings.Join(names, ", "))
	}
	return fmt.Sprintf("prompt matched: score %d (%s)", score, strings.Join(names, ", "))
}

func RecordPromptFilterRejectLog(c *gin.Context, endpoint string, verdict PromptFilterVerdict) {
	settings := system_setting.GetPromptFilterSettings()
	if !settings.LogMatches || c == nil {
		return
	}
	if c.GetInt("id") == 0 && c.GetInt("token_id") == 0 {
		return
	}
	other := map[string]interface{}{
		"code":              settings.BlockErrorCode,
		"source":            "local_filter",
		"method":            c.Request.Method,
		"path":              c.Request.URL.Path,
		"endpoint":          endpoint,
		"status_code":       promptFilterLogStatusCode(verdict.Action, settings),
		"action":            verdict.Action,
		"score":             verdict.Score,
		"raw_score":         verdict.RawScore,
		"threshold":         verdict.Threshold,
		"strict_hit":        verdict.StrictHit,
		"matched":           promptFilterLogMatches(verdict.Matched),
		"text_preview":      RedactedPromptFilterPreview(verdict.TextPreview, promptFilterLogTextPreviewRunes),
		"extracted_chars":   verdict.ExtractedChars,
		"prompt_filter_msg": verdict.Reason,
		"mode":              verdict.Mode,
		"channel_id":        common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		"reviewed":          verdict.Reviewed,
		"review_flagged":    verdict.ReviewFlagged,
		"review_model":      verdict.ReviewModel,
		"review_error":      verdict.ReviewError,
	}
	if fullText := promptFilterLogFullText(verdict); fullText != "" {
		other["admin_info"] = map[string]interface{}{
			"prompt_filter_full_text": fullText,
		}
	}
	model.RecordPromptFilterRejectLog(c, settings.Message, other)
}

func promptFilterLogFullText(verdict PromptFilterVerdict) string {
	if verdict.Action == PromptFilterActionAllow || strings.TrimSpace(verdict.FullText) == "" {
		return ""
	}
	return verdict.FullText
}

func promptFilterLogMatches(matches []PromptFilterMatch) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(matches))
	for _, match := range matches {
		item := map[string]interface{}{
			"name":     match.Name,
			"weight":   match.Weight,
			"category": match.Category,
			"strict":   match.Strict,
		}
		term := promptFilterPreview(RedactPromptFilterSensitive(match.Term), promptFilterMatchTermMaxRunes)
		if term != "" {
			item["term"] = term
		}
		result = append(result, item)
	}
	return result
}

func promptFilterPatternMatchTerm(re *regexp.Regexp, text string) (string, bool) {
	if re == nil {
		return "", false
	}
	indexes := re.FindStringIndex(text)
	if indexes == nil {
		return "", false
	}
	if len(indexes) != 2 || indexes[0] < 0 || indexes[1] < indexes[0] || indexes[1] > len(text) {
		return "", true
	}
	return promptFilterPreview(text[indexes[0]:indexes[1]], promptFilterMatchTermMaxRunes), true
}

func promptFilterLogStatusCode(action string, settings system_setting.PromptFilterSettings) int {
	if action == PromptFilterActionBlock {
		return settings.BlockStatusCode
	}
	return http.StatusOK
}
