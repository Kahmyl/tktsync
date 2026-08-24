ALTER TABLE app_users
ADD COLUMN email text;

CREATE UNIQUE INDEX app_users_email_uq
ON app_users (lower(email))
WHERE email IS NOT NULL;

ALTER TABLE app_users
ADD CONSTRAINT app_users_email_normalized_ck
CHECK (email IS NULL OR (email = lower(btrim(email)) AND email <> ''));
