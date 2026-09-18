-- Expand before upstream 235 renames models_list_config. The migration runner
-- orders full filenames, including unapplied files below the highest recorded
-- prefix. Keep both names while the production/rollback image still uses the
-- old name. Do not remove this bridge until those images are retired.
--
-- This also repairs a database that has already run upstream 235 and therefore
-- contains only model_allowlist. Existing nonempty contradictory values abort
-- the migration rather than choosing one configuration and losing the other.
-- SQL NULL is an empty configuration; JSON null remains a JSON value.
LOCK TABLE groups IN ACCESS EXCLUSIVE MODE;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM groups
         WHERE COALESCE(models_list_config, '{}'::jsonb) <> '{}'::jsonb
           AND COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb
           AND models_list_config IS DISTINCT FROM model_allowlist
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'conflicting legacy and current group model configurations';
    END IF;
END
$$;

UPDATE groups
   SET models_list_config = CASE
           WHEN COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb THEN model_allowlist
           ELSE COALESCE(models_list_config, '{}'::jsonb)
       END,
       model_allowlist = CASE
           WHEN COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb THEN model_allowlist
           ELSE COALESCE(models_list_config, '{}'::jsonb)
       END
 WHERE models_list_config IS NULL
    OR model_allowlist IS NULL
    OR models_list_config IS DISTINCT FROM model_allowlist;

ALTER TABLE groups
    ALTER COLUMN models_list_config SET DEFAULT '{}'::jsonb,
    ALTER COLUMN models_list_config SET NOT NULL,
    ALTER COLUMN model_allowlist SET DEFAULT '{}'::jsonb,
    ALTER COLUMN model_allowlist SET NOT NULL;

CREATE OR REPLACE FUNCTION sub2api_sync_group_model_allowlist_compat()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    legacy_changed BOOLEAN;
    current_changed BOOLEAN;
BEGIN
    NEW.models_list_config := COALESCE(NEW.models_list_config, '{}'::jsonb);
    NEW.model_allowlist := COALESCE(NEW.model_allowlist, '{}'::jsonb);

    IF TG_OP = 'INSERT' THEN
        -- A writer knows only one name; the other receives its empty default.
        -- A nonempty value wins against an empty/default value on INSERT.
        IF NEW.models_list_config = '{}'::jsonb THEN
            NEW.models_list_config := NEW.model_allowlist;
        ELSIF NEW.model_allowlist = '{}'::jsonb THEN
            NEW.model_allowlist := NEW.models_list_config;
        ELSIF NEW.models_list_config IS DISTINCT FROM NEW.model_allowlist THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'conflicting legacy and current group model configurations';
        END IF;
    ELSE
        legacy_changed := NEW.models_list_config IS DISTINCT FROM OLD.models_list_config;
        current_changed := NEW.model_allowlist IS DISTINCT FROM OLD.model_allowlist;

        IF legacy_changed AND current_changed THEN
            -- A same-statement clear and nonempty edit is ambiguous too.
            IF NEW.models_list_config IS DISTINCT FROM NEW.model_allowlist THEN
                RAISE EXCEPTION USING
                    ERRCODE = '23514',
                    MESSAGE = 'conflicting legacy and current group model configurations';
            END IF;
        ELSIF legacy_changed THEN
            NEW.model_allowlist := NEW.models_list_config;
        ELSIF current_changed THEN
            NEW.models_list_config := NEW.model_allowlist;
        END IF;
    END IF;

    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_groups_model_allowlist_compat ON groups;
CREATE TRIGGER trg_groups_model_allowlist_compat
BEFORE INSERT OR UPDATE ON groups
FOR EACH ROW EXECUTE FUNCTION sub2api_sync_group_model_allowlist_compat();

COMMENT ON COLUMN groups.models_list_config IS
    'Legacy name synchronized with model_allowlist for rolling upgrade and image rollback';
