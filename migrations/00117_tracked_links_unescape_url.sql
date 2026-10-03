-- A tracked link is the URL a browser follows, not the HTML attribute
-- that carried it. Rows captured as written still hold the escaped
-- ampersand and redirect to it, so it is decoded in place. The hash is
-- left alone, because delivered messages name the row by it.
--
-- "&amp;amp;" decodes to "&amp;", the literal text it stood for, since
-- each match is replaced once from left to right.

-- +goose Up
UPDATE tracked_links
SET original_url = regexp_replace(original_url, '&(amp|#0*38|#x0*26);', '&', 'gi')
WHERE original_url ~* '&(amp|#0*38|#x0*26);';

-- +goose Down
SELECT 1;
