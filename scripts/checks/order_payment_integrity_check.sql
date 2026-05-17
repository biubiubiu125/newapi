SELECT trade_no, status, payment_provider, payment_method, paid_amount, paid_currency, amount
FROM top_ups
WHERE trade_no LIKE 'AUDIT%' OR trade_no LIKE 'EPU%' OR trade_no LIKE 'SUB%'
ORDER BY id DESC
LIMIT 200;

SELECT trade_no, COUNT(*) AS duplicate_orders
FROM top_ups
GROUP BY trade_no
HAVING COUNT(*) > 1;

SELECT source_trade_no, COUNT(*) AS commission_count
FROM referral_commissions
GROUP BY source_trade_no
HAVING COUNT(*) > 1;

SELECT t.trade_no, t.status, t.payment_provider, t.paid_amount, t.paid_currency, rc.id AS commission_id
FROM top_ups t
LEFT JOIN referral_commissions rc ON rc.source_trade_no = t.trade_no
WHERE t.status = 'success'
ORDER BY t.id DESC
LIMIT 200;
