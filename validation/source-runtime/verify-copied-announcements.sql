-- Original IDs are frozen before migrations; only hashes and counts are emitted.
SELECT 'announcements|' || count(*) || '|' || COALESCE(md5(string_agg(md5((to_jsonb(a) - ARRAY['source_type','source_id','source_event_kind','source_revision'])::text),'' ORDER BY id)), 'empty')
FROM announcements a WHERE id <= :announcement_max_id
UNION ALL
SELECT 'unexpected_announcement_source|' || count(*)
FROM announcements a WHERE id <= :announcement_max_id AND (
    COALESCE(to_jsonb(a)->'source_type','null'::jsonb) <> 'null'::jsonb
    OR COALESCE(to_jsonb(a)->'source_id','null'::jsonb) <> 'null'::jsonb
    OR COALESCE(to_jsonb(a)->'source_event_kind','null'::jsonb) <> 'null'::jsonb
    OR COALESCE((to_jsonb(a)->>'source_revision')::integer,0) <> 0)
UNION ALL
SELECT 'protected_announcement_schema|' || md5(string_agg(ROW(column_name,data_type,is_nullable,column_default)::text,'' ORDER BY ordinal_position))
FROM information_schema.columns WHERE table_schema='public' AND table_name='announcements'
AND column_name NOT IN ('source_type','source_id','source_event_kind','source_revision');
