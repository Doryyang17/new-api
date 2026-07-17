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

import React, { useEffect, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../../helpers/quota';

export default function SettingsCheckin(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    'checkin_setting.enabled': false,
    'checkin_setting.min_quota': 1000,
    'checkin_setting.max_quota': 10000,
    'checkin_bonus_setting.enabled': false,
    'checkin_bonus_setting.min_amount': 0.1,
    'checkin_bonus_setting.max_amount': 1,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    if (
      inputs['checkin_bonus_setting.min_amount'] >
      inputs['checkin_bonus_setting.max_amount']
    ) {
      return showError('最低签到赠金不能高于最高签到赠金');
    }
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const bonusMinKey = 'checkin_bonus_setting.min_amount';
    const bonusMaxKey = 'checkin_bonus_setting.max_amount';
    const bonusUpdates = updateArray.filter(
      (item) => item.key === bonusMinKey || item.key === bonusMaxKey,
    );
    const orderedUpdates = updateArray.filter(
      (item) => item.key !== bonusMinKey && item.key !== bonusMaxKey,
    );
    if (
      bonusUpdates.length === 2 &&
      inputs[bonusMinKey] > inputsRow[bonusMaxKey]
    ) {
      orderedUpdates.push(
        bonusUpdates.find((item) => item.key === bonusMaxKey),
        bonusUpdates.find((item) => item.key === bonusMinKey),
      );
    } else {
      orderedUpdates.push(
        ...bonusUpdates.sort((a, b) =>
          a.key === bonusMinKey ? -1 : b.key === bonusMinKey ? 1 : 0,
        ),
      );
    }
    const updateOption = (item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else if (
        item.key === 'checkin_bonus_setting.min_amount' ||
        item.key === 'checkin_bonus_setting.max_amount'
      ) {
        value = String(displayAmountToQuota(inputs[item.key]));
      } else {
        value = String(inputs[item.key]);
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    };
    setLoading(true);
    orderedUpdates
      .reduce(
        (chain, item) =>
          chain.then(async (responses) => [
            ...responses,
            await updateOption(item),
          ]),
        Promise.resolve([]),
      )
      .then((responses) => {
        if (orderedUpdates.length === 1) {
          if (responses.includes(undefined)) return;
        } else if (orderedUpdates.length > 1) {
          if (responses.includes(undefined))
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
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        if (
          key === 'checkin_bonus_setting.min_amount' ||
          key === 'checkin_bonus_setting.max_amount'
        ) {
          currentInputs[key] = quotaToDisplayAmount(props.options[key]);
        } else {
          currentInputs[key] = props.options[key];
        }
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
          <Form.Section text={t('签到设置')}>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t('签到功能允许用户每日签到获取随机额度奖励')}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_setting.enabled'}
                  label={t('启用签到功能')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('checkin_setting.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_setting.min_quota'}
                  label={t('签到最小额度')}
                  placeholder={t('签到奖励的最小额度')}
                  onChange={handleFieldChange('checkin_setting.min_quota')}
                  min={0}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    inputs['checkin_bonus_setting.enabled']
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_setting.max_quota'}
                  label={t('签到最大额度')}
                  placeholder={t('签到奖励的最大额度')}
                  onChange={handleFieldChange('checkin_setting.max_quota')}
                  min={0}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    inputs['checkin_bonus_setting.enabled']
                  }
                />
              </Col>
            </Row>
            <Typography.Text
              strong
              style={{ marginTop: 20, marginBottom: 8, display: 'block' }}
            >
              独立签到赠金
            </Typography.Text>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              开启后仅发放独立赠金，不再发放账户余额奖励；赠金当天 24:00
              自动失效并优先抵扣消费
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_bonus_setting.enabled'}
                  label='启用签到赠金'
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('checkin_bonus_setting.enabled')}
                  disabled={!inputs['checkin_setting.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_bonus_setting.min_amount'}
                  label='最低赠金金额'
                  placeholder='0.1'
                  onChange={handleFieldChange(
                    'checkin_bonus_setting.min_amount',
                  )}
                  min={0}
                  step={0.01}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    !inputs['checkin_bonus_setting.enabled']
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_bonus_setting.max_amount'}
                  label='最高赠金金额'
                  placeholder='1'
                  onChange={handleFieldChange(
                    'checkin_bonus_setting.max_amount',
                  )}
                  min={0}
                  step={0.01}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    !inputs['checkin_bonus_setting.enabled']
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存签到设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
