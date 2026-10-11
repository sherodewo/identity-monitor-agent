ALTER TABLE mention_media ADD COLUMN duplicate_of INTEGER REFERENCES mention_media(id) ON DELETE SET NULL;
CREATE INDEX media_mention ON mention_media(mention_id);
CREATE INDEX run_subject_source ON collection_runs(subject_id,source,status);
