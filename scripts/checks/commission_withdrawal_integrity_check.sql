SELECT affiliate_id, user_id, available_amount, frozen_amount, withdrawn_amount, pending_amount
FROM referral_commission_accounts
ORDER BY affiliate_id;

SELECT id, affiliate_id, user_id, amount, status, idempotency_key
FROM referral_withdrawals
ORDER BY id DESC
LIMIT 200;

SELECT external_ref_id, COUNT(*) AS duplicate_ledger_refs
FROM referral_commission_ledgers
WHERE external_ref_id <> ''
GROUP BY external_ref_id
HAVING COUNT(*) > 1;

SELECT w.id, w.amount, COALESCE(SUM(wi.amount), 0) AS allocated_amount, w.status
FROM referral_withdrawals w
LEFT JOIN referral_withdrawal_items wi ON wi.withdrawal_id = w.id
GROUP BY w.id, w.amount, w.status
HAVING w.status IN ('submitted', 'approved', 'paid') AND ABS(w.amount - COALESCE(SUM(wi.amount), 0)) > 0.000001;
