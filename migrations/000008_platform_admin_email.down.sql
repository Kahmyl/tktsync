ALTER TABLE app_users
DROP CONSTRAINT app_users_email_normalized_ck;

DROP INDEX app_users_email_uq;

ALTER TABLE app_users
DROP COLUMN email;
