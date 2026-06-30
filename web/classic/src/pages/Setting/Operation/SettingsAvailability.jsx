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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const defaultInputs = {
  'availability_setting.enabled': false,
  'availability_setting.unavailable_start': '22:00',
  'availability_setting.unavailable_end': '08:00',
  'availability_setting.timezone': 'Asia/Shanghai',
  'availability_setting.message':
    '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~',
};

const timePattern = /^([01]\d|2[0-3]):[0-5]\d$/;

export default function SettingsAvailability(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultInputs);
  const [inputsRow, setInputsRow] = useState(defaultInputs);
  const refForm = useRef();

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    if (
      !timePattern.test(inputs['availability_setting.unavailable_start']) ||
      !timePattern.test(inputs['availability_setting.unavailable_end'])
    ) {
      return showError(t('时间格式必须为 HH:MM'));
    }
    if (!inputs['availability_setting.timezone']) {
      return showError(t('时区不能为空'));
    }
    if (!inputs['availability_setting.message']) {
      return showError(t('提示文案不能为空'));
    }
    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: String(inputs[item.key]),
      }),
    );
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = { ...defaultInputs };
    for (let key in props.options) {
      if (Object.keys(defaultInputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('系统可用时间')}>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t('开启后，服务将在配置时间段内拒绝 API 调用并显示宵禁主页')}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'availability_setting.enabled'}
                  label={t('启用宵禁')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('availability_setting.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'availability_setting.unavailable_start'}
                  label={t('开始时间')}
                  placeholder='22:00'
                  extraText={t('24 小时制，例如 22:00')}
                  onChange={handleFieldChange(
                    'availability_setting.unavailable_start',
                  )}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'availability_setting.unavailable_end'}
                  label={t('结束时间')}
                  placeholder='08:00'
                  extraText={t('24 小时制，例如 08:00')}
                  onChange={handleFieldChange(
                    'availability_setting.unavailable_end',
                  )}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'availability_setting.timezone'}
                  label={t('判定时区')}
                  placeholder='Asia/Shanghai'
                  onChange={handleFieldChange('availability_setting.timezone')}
                />
              </Col>
              <Col xs={24} sm={24} md={16} lg={16} xl={16}>
                <Form.TextArea
                  field={'availability_setting.message'}
                  label={t('提示文案')}
                  autosize={{ minRows: 2, maxRows: 4 }}
                  onChange={handleFieldChange('availability_setting.message')}
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存系统可用时间设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
