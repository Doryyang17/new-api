# Curated Prompt Filter Presets

These three preset lexicons are locally curated from the legacy Konsheng Sensitive-lexicon upstream material.

The old full upstream lexicon files are intentionally not bundled as runtime presets. These files intentionally keep only the requested policy scope:

- `精简-涉政.txt`: China-sensitive politics, including national leaders and aliases, sensitive political events, and sensitive political organizations.
- `精简-色情.txt`: explicit sexual content.
- `精简-暴力.txt`: violence, terror, guns, explosives, and weapon-making terms.

Source handling notes from the one-time audit:

- Discarded as runtime presets: `非法网址.txt`, `新思想启蒙.txt`, `广告类型.txt`, and `其他词库.txt`.
- Partially mined for China-sensitive politics or violence only: `零时-Tencent.txt`, `GFW补充词库.txt`, `网易前端过滤敏感词库.txt`, `补充词库.txt`, `贪腐词库.txt`, `民生词库.txt`, and `COVID-19词库.txt`.
- Merged for explicit sexual content: `色情类型.txt` and `色情词库.txt`.
- Merged for violence and terror: `涉枪涉爆.txt` and the violence-only parts of `暴恐词库.txt`.

Excluded by design: single-character terms, common business/product words, generic words such as 信息/系统/网站/政府, URL/domain blacklists, advertising, fraud, gambling, drug-only entries, and old mixed joke/slogan phrases.

License note: the upstream material was published under the MIT License by Konsheng. The license notice is retained in `LICENSE` in this directory.
