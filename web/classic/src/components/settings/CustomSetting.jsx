/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState } from 'react';
import { Card, Spin } from '@douyinfe/semi-ui';
import SettingsAvailability from '../../pages/Setting/Operation/SettingsAvailability';
import SettingsSensitiveWords from '../../pages/Setting/Operation/SettingsSensitiveWords';
import SettingsCheckin from '../../pages/Setting/Operation/SettingsCheckin';
import { API, showError, toBoolean } from '../../helpers';

const defaultInputs = {
  'availability_setting.enabled': false,
  'availability_setting.unavailable_start': '22:00',
  'availability_setting.unavailable_end': '08:00',
  'availability_setting.timezone': 'Asia/Shanghai',
  'availability_setting.message':
    '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~',
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  'checkin_setting.enabled': false,
  'checkin_setting.min_quota': 1000,
  'checkin_setting.max_quota': 10000,
  'checkin_bonus_setting.enabled': false,
  'checkin_bonus_setting.min_amount': 50000,
  'checkin_bonus_setting.max_amount': 500000,
};

const CustomSetting = () => {
  const [inputs, setInputs] = useState(defaultInputs);
  const [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      const newInputs = { ...defaultInputs };
      data.forEach((item) => {
        if (!Object.prototype.hasOwnProperty.call(defaultInputs, item.key)) {
          return;
        }
        if (typeof defaultInputs[item.key] === 'boolean') {
          newInputs[item.key] = toBoolean(item.value);
        } else {
          newInputs[item.key] = item.value;
        }
      });
      setInputs(newInputs);
    } else {
      showError(message);
    }
  };

  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
    } catch (error) {
      showError('刷新失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  return (
    <Spin spinning={loading} size='large'>
      <Card style={{ marginTop: '10px' }}>
        <SettingsAvailability options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsSensitiveWords options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsCheckin options={inputs} refresh={onRefresh} />
      </Card>
    </Spin>
  );
};

export default CustomSetting;
