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
import {
  Banner,
  Button,
  Card,
  Empty,
  Input,
  Space,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../helpers';

const { Text, Title } = Typography;

function createRow(index = 0) {
  return {
    id: `provider-price-${Date.now()}-${index}`,
    group_name: '',
    model_name: '',
    input_price: '',
    output_price: '',
    cache_input_price: '',
    cache_create_price: '',
    cache_create_price_1h: '',
    image_output_price: '',
    enabled: true,
    note: '',
    sort_order: index,
  };
}

function toText(value) {
  return value == null ? '' : String(value);
}

function parsePrice(value) {
  const trimmed = String(value ?? '').trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
}

export default function ProviderPricing() {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [rows, setRows] = useState([]);

  async function load() {
    setLoading(true);
    try {
      const res = await API.get('/api/user/admin/provider-pricing');
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      const items = Array.isArray(res.data.data?.items)
        ? res.data.data.items
        : [];
      setRows(
        items.map((item, index) => ({
          id: item.id || `provider-price-${index}`,
          group_name: item.group_name || '',
          model_name: item.model_name || '',
          input_price: toText(item.input_price),
          output_price: toText(item.output_price),
          cache_input_price: toText(item.cache_input_price),
          cache_create_price: toText(item.cache_create_price),
          cache_create_price_1h: toText(item.cache_create_price_1h),
          image_output_price: toText(item.image_output_price),
          enabled: item.enabled !== false,
          note: item.note || '',
          sort_order: item.sort_order ?? index,
        })),
      );
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  function updateRow(index, key, value) {
    setRows((prev) =>
      prev.map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row,
      ),
    );
  }

  function addRow() {
    setRows((prev) => [...prev, createRow(prev.length)]);
  }

  function removeRow(index) {
    setRows((prev) => prev.filter((_, rowIndex) => rowIndex !== index));
  }

  function hasInvalidExportRow(rows) {
    const seenModelGroups = new Set();
    return rows.some((row) => {
      const groupName = row.group_name.trim();
      const modelName = row.model_name.trim();
      if (!groupName || !modelName) {
        return false;
      }
      const modelGroupKey = `${modelName.toLowerCase()}\u0000${groupName.toLowerCase()}`;
      if (seenModelGroups.has(modelGroupKey)) {
        return true;
      }
      seenModelGroups.add(modelGroupKey);
      const inputPrice = parsePrice(row.input_price);
      if (inputPrice == null || inputPrice <= 0) {
        return true;
      }
      return [
        row.output_price,
        row.cache_input_price,
        row.cache_create_price,
        row.cache_create_price_1h,
        row.image_output_price,
      ].some((value) => {
        const price = parsePrice(value);
        return price != null && price < 0;
      });
    });
  }

  async function save() {
    if (hasInvalidExportRow(rows)) {
      showError('每个导出项都必须有唯一的模型和分组组合，输入价格需大于 0，且不能包含负价格');
      return;
    }
    setSaving(true);
    try {
      const payload = rows
        .map((row, index) => {
          const inputPrice = parsePrice(row.input_price);
          const outputPrice = parsePrice(row.output_price);
          return {
            id: row.id,
            group_name: row.group_name.trim(),
            model_name: row.model_name.trim(),
            input_price: inputPrice,
            output_price: outputPrice ?? inputPrice,
            cache_input_price: parsePrice(row.cache_input_price),
            cache_create_price: parsePrice(row.cache_create_price),
            cache_create_price_1h: parsePrice(row.cache_create_price_1h),
            image_output_price: parsePrice(row.image_output_price),
            enabled: row.enabled,
            note: row.note.trim(),
            sort_order: Number.isFinite(Number(row.sort_order))
              ? Number(row.sort_order)
              : index,
          };
        })
        .filter((row) => row.group_name && row.model_name);

      const res = await API.put('/api/user/admin/provider-pricing', {
        items: payload,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess('公开价格导出配置已保存');
      await load();
    } catch (error) {
      showError(error);
    } finally {
      setSaving(false);
    }
  }

  const priceFields = [
    ['input_price', '输入价格'],
    ['output_price', '输出价格'],
    ['cache_input_price', '缓存读取价格'],
    ['cache_create_price', '缓存创建价格'],
    ['cache_create_price_1h', '1h 缓存创建价格'],
    ['image_output_price', '图片输出价格'],
  ];

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2'>
      <div className='space-y-6'>
        <div className='space-y-2'>
          <Title heading={3} style={{ marginBottom: 0 }}>
            公开价格导出配置
          </Title>
          <Text type='tertiary'>
            在这里维护 `/api/provider/pricing`
            的公开展示价格。这份价格只提供给外部网站读取展示，不参与真实计费，也不会影响实际扣费。
          </Text>
        </div>

        <Card className='!rounded-2xl shadow-sm border-0'>
          <Space vertical style={{ width: '100%' }} spacing='loose'>
            <Banner
              type='info'
              description='导出金额单位固定为 CNY / 1M tokens。你可以把这里理解成“公开展示价清单”，而不是实际计费规则。'
              fullMode={false}
            />

            <div className='flex flex-wrap items-center justify-between gap-4'>
              <div className='flex items-center gap-3'>
                <Tag color='blue' shape='circle'>
                  当前导出项 {rows.length}
                </Tag>
                <Tag color='green' shape='circle'>
                  导出单位 CNY / 1M tokens
                </Tag>
              </div>

              <div className='flex flex-wrap items-center gap-3'>
                <Button
                  onClick={() => void load()}
                  disabled={loading || saving}
                >
                  刷新
                </Button>
                <Button onClick={addRow} disabled={saving}>
                  新增导出项
                </Button>
                <Button
                  type='primary'
                  theme='solid'
                  onClick={save}
                  loading={saving}
                >
                  保存配置
                </Button>
              </div>
            </div>

            {rows.length === 0 ? (
              <Card className='!rounded-2xl shadow-sm border-0 bg-[var(--semi-color-fill-0)]'>
                <Empty
                  description='当前还没有公开价格导出项。新增后，别的网站即可通过 /api/provider/pricing 读取展示价格。'
                  image={null}
                />
              </Card>
            ) : (
              <div className='space-y-4'>
                {rows.map((row, index) => (
                  <Card
                    key={row.id}
                    className='!rounded-2xl shadow-sm border-0'
                    bodyStyle={{ padding: 18 }}
                  >
                    <Space vertical style={{ width: '100%' }} spacing='loose'>
                      <div className='flex flex-wrap items-center justify-between gap-3'>
                        <div className='flex items-center gap-3'>
                          <Text strong>导出项 {index + 1}</Text>
                          <Tag
                            color={row.enabled ? 'green' : 'grey'}
                            shape='circle'
                          >
                            {row.enabled ? '已启用' : '已关闭'}
                          </Tag>
                        </div>

                        <Button
                          type='danger'
                          theme='borderless'
                          onClick={() => removeRow(index)}
                        >
                          删除
                        </Button>
                      </div>

                      <div className='space-y-3'>
                        <Text type='tertiary'>基础信息</Text>
                        <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
                          <Input
                            value={row.group_name}
                            onChange={(value) =>
                              updateRow(index, 'group_name', value)
                            }
                            addonBefore='分组名称'
                          />
                          <Input
                            value={row.model_name}
                            onChange={(value) =>
                              updateRow(index, 'model_name', value)
                            }
                            addonBefore='模型名称'
                          />
                        </div>
                      </div>

                      <div className='space-y-3'>
                        <div className='flex items-center justify-between gap-3'>
                          <Text type='tertiary'>价格信息</Text>
                          <Text type='tertiary'>单位：CNY / 1M tokens</Text>
                        </div>

                        <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4'>
                          {priceFields.map(([field, label]) => (
                            <div
                              key={field}
                              className='rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] p-3'
                            >
                              <Text type='tertiary' className='block mb-2'>
                                {label}
                              </Text>
                              <Input
                                value={row[field]}
                                onChange={(value) =>
                                  updateRow(index, field, value)
                                }
                                suffix='CNY'
                              />
                            </div>
                          ))}
                        </div>
                      </div>

                      <div className='space-y-3'>
                        <Text type='tertiary'>备注与状态</Text>
                        <div className='grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_180px_150px] gap-4 items-center'>
                          <Input
                            value={row.note}
                            onChange={(value) =>
                              updateRow(index, 'note', value)
                            }
                            addonBefore='备注'
                          />
                          <Input
                            value={toText(row.sort_order)}
                            onChange={(value) =>
                              updateRow(index, 'sort_order', value)
                            }
                            addonBefore='排序'
                          />
                          <div className='flex items-center justify-center gap-3 rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] px-4 py-3'>
                            <Text>启用</Text>
                            <Switch
                              checked={row.enabled}
                              onChange={(checked) =>
                                updateRow(index, 'enabled', checked)
                              }
                            />
                          </div>
                        </div>
                      </div>
                    </Space>
                  </Card>
                ))}
              </div>
            )}
          </Space>
        </Card>
      </div>
    </div>
  );
}
