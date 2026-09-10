-- Adopt the site brand only for installations still using the upstream name.
-- Explicit custom site names and logos remain administrator-controlled.
UPDATE settings
SET value = '水上列车', updated_at = NOW()
WHERE key = 'site_name' AND (value = 'Sub2API' OR BTRIM(value) = '');

UPDATE settings
SET value = '', updated_at = NOW()
WHERE key = 'site_logo' AND value = '/logo.svg';
