BEGIN;

WITH seed_input AS (
    SELECT
        :'auth_subject'::uuid::text AS auth_subject,
        NULLIF(btrim(:'display_name'), '') AS display_name
),
upserted_user AS (
    INSERT INTO app_users (
        auth_provider,
        auth_subject,
        display_name,
        state
    )
    SELECT
        'supabase',
        auth_subject,
        display_name,
        'ACTIVE'
    FROM seed_input
    ON CONFLICT (auth_provider, auth_subject) DO UPDATE
    SET
        display_name = EXCLUDED.display_name,
        updated_at = now()
    WHERE app_users.state = 'ACTIVE'
      AND app_users.display_name IS DISTINCT FROM EXCLUDED.display_name
    RETURNING id, state
),
operator_user AS (
    SELECT id, state
    FROM upserted_user

    UNION ALL

    SELECT app_users.id, app_users.state
    FROM app_users
    CROSS JOIN seed_input
    WHERE app_users.auth_provider = 'supabase'
      AND app_users.auth_subject = seed_input.auth_subject
      AND NOT EXISTS (SELECT 1 FROM upserted_user)
)
INSERT INTO platform_user_roles (user_id, role)
SELECT id, 'PLATFORM_ADMIN'
FROM operator_user
WHERE state = 'ACTIVE'
  AND :'platform_admin'::boolean
ON CONFLICT (user_id, role) DO NOTHING;

COMMIT;
