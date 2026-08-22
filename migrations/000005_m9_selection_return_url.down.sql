ALTER TABLE buyer_selection_sessions
    DROP CONSTRAINT buyer_selection_sessions_return_url_ck,
    DROP COLUMN return_url;
