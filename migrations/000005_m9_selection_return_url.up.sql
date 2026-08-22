ALTER TABLE buyer_selection_sessions
    ADD COLUMN return_url text NOT NULL;

ALTER TABLE buyer_selection_sessions
    ADD CONSTRAINT buyer_selection_sessions_return_url_ck
    CHECK (return_url ~ '^https://');
