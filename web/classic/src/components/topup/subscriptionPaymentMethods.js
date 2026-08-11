/*
Copyright (C) 2026 QuantumNous

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

// 仅保留由订阅易支付接口处理的方式。
export function getEpayMethods(payMethods = []) {
  return (payMethods || []).filter((m) => {
    const type = (m?.type || '').trim();
    return (
      type &&
      type !== 'usdt' &&
      type !== 'stripe' &&
      type !== 'creem' &&
      type !== 'waffo' &&
      type !== 'waffo_pancake' &&
      !type.startsWith('waffo:')
    );
  });
}

export function isSafeHttpCheckoutUrl(value) {
  const trimmed = (value || '').trim();
  if (!trimmed) return false;
  if (trimmed.includes('\\')) return false;
  try {
    const url = new URL(trimmed);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

export function openSafeCheckoutWindow(url, opener = window.open) {
  const checkoutUrl = (url || '').trim();
  if (!isSafeHttpCheckoutUrl(checkoutUrl)) return false;
  opener(checkoutUrl, '_blank', 'noopener,noreferrer');
  return true;
}

export function hasConfiguredPaymentId(value) {
  return typeof value === 'string' && value.trim() !== '';
}

export function getSubscriptionPaymentTradeNo(response) {
  const candidates = [
    response?.trade_no,
    response?.order_id,
    response?.data?.trade_no,
    response?.data?.order_id,
  ];
  return candidates
    .find((value) => typeof value === 'string' && value.trim() !== '')
    ?.trim();
}

export function buildPendingSubscriptionPayment(response, details = {}) {
  const tradeNo = getSubscriptionPaymentTradeNo(response);
  if (!tradeNo) return null;
  return {
    tradeNo,
    title: details.title || '',
    amount: Number(details.amount || 0),
    paymentMethod: details.paymentMethod || '',
  };
}

export function isSubscriptionPaymentCompleted(history, tradeNo) {
  if (!tradeNo) return false;
  const items = history?.items || history?.data?.items || [];
  return items.some(
    (record) => record?.trade_no === tradeNo && record?.status === 'success',
  );
}
