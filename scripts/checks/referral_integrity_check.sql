-- Set :run_prefix to a value such as load_invitee_ before running in psql.
SELECT COUNT(*) AS load_users
FROM users
WHERE username LIKE :'run_prefix' || '%';

SELECT COUNT(*) AS users_without_binding
FROM users u
LEFT JOIN referral_bindings rb ON rb.invitee_user_id = u.id
WHERE u.username LIKE :'run_prefix' || '%'
  AND rb.id IS NULL;

SELECT rb.inviter_user_id, COUNT(*) AS invitees
FROM users u
JOIN referral_bindings rb ON rb.invitee_user_id = u.id
WHERE u.username LIKE :'run_prefix' || '%'
GROUP BY rb.inviter_user_id
ORDER BY rb.inviter_user_id;

SELECT email, COUNT(*) AS duplicates
FROM users
WHERE email LIKE :'run_prefix' || '%@example.test'
GROUP BY email
HAVING COUNT(*) > 1;
