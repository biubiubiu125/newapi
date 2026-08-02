import { Modal } from '@douyinfe/semi-ui';
import { expect, test } from 'bun:test';
import React from 'react';

import ChannelsActions from './ChannelsActions.jsx';

const noop = () => {};

const baseProps = (overrides = {}) => ({
  enableBatchDelete: false,
  batchDeleteChannels: noop,
  setShowBatchSetTag: noop,
  testAllChannels: noop,
  fixChannelsAbilities: noop,
  updateAllChannelsBalance: noop,
  deleteAllDisabledChannels: noop,
  applyAllUpstreamUpdates: noop,
  detectAllUpstreamUpdates: noop,
  detectAllUpstreamUpdatesLoading: false,
  applyAllUpstreamUpdatesLoading: false,
  canDetectUpstreamUpdates: false,
  canApplyUpstreamUpdates: false,
  channelPermissions: {},
  compactMode: false,
  setCompactMode: noop,
  idSort: false,
  setIdSort: noop,
  setEnableBatchDelete: noop,
  enableTagMode: false,
  setEnableTagMode: noop,
  statusFilter: 'all',
  setStatusFilter: noop,
  getFormValues: () => ({ searchKeyword: '', searchGroup: '', searchModel: '' }),
  loadChannels: noop,
  searchChannels: noop,
  activeTypeKey: 'all',
  activePage: 1,
  pageSize: 10,
  setActivePage: noop,
  t: (value) => value,
  ...overrides,
});

function walkReactTree(value, visit) {
  if (Array.isArray(value)) {
    for (const item of value) walkReactTree(item, visit);
    return;
  }
  if (!React.isValidElement(value)) return;

  visit(value);
  walkReactTree(value.props?.children, visit);
  walkReactTree(value.props?.render, visit);
}

function findRepairButton(root) {
  let match = null;
  walkReactTree(root, (element) => {
    const children = React.Children.toArray(element.props?.children);
    if (children.includes('修复数据库一致性')) {
      match = element;
    }
  });
  return match;
}

test('classic channel repair action is disabled and inert without operate permission', () => {
  const confirmCalls = [];
  const originalConfirm = Modal.confirm;
  Modal.confirm = (options) => {
    confirmCalls.push(options);
  };
  try {
    const repairButton = findRepairButton(
      ChannelsActions(
        baseProps({
          channelPermissions: { canOperateChannel: false },
        }),
      ),
    );

    expect(repairButton).not.toBeNull();
    expect(repairButton.props.disabled).toBe(true);
    repairButton.props.onClick();
    expect(confirmCalls).toHaveLength(0);
  } finally {
    Modal.confirm = originalConfirm;
  }
});

test('classic channel repair action confirms and runs with operate permission', () => {
  const confirmCalls = [];
  let repairCalls = 0;
  const originalConfirm = Modal.confirm;
  Modal.confirm = (options) => {
    confirmCalls.push(options);
  };
  try {
    const repairButton = findRepairButton(
      ChannelsActions(
        baseProps({
          channelPermissions: { canOperateChannel: true },
          fixChannelsAbilities: () => {
            repairCalls += 1;
          },
        }),
      ),
    );

    expect(repairButton).not.toBeNull();
    expect(repairButton.props.disabled).toBe(false);
    repairButton.props.onClick();
    expect(confirmCalls).toHaveLength(1);
    confirmCalls[0].onOk();
    expect(repairCalls).toBe(1);
  } finally {
    Modal.confirm = originalConfirm;
  }
});
