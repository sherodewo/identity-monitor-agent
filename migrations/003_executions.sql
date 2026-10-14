CREATE TABLE executions(id TEXT PRIMARY KEY, created_at TEXT NOT NULL, label TEXT NOT NULL, total_queries INTEGER NOT NULL);
ALTER TABLE collection_runs ADD COLUMN execution_id TEXT REFERENCES executions(id);
CREATE INDEX runs_execution ON collection_runs(execution_id);
CREATE TABLE execution_mentions(execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE, mention_id INTEGER NOT NULL REFERENCES mentions(id) ON DELETE CASCADE, PRIMARY KEY(execution_id,mention_id));
CREATE TABLE run_mentions(run_id TEXT NOT NULL REFERENCES collection_runs(id) ON DELETE CASCADE, mention_id INTEGER NOT NULL REFERENCES mentions(id) ON DELETE CASCADE, PRIMARY KEY(run_id,mention_id));
INSERT INTO executions SELECT 'legacy',strftime('%Y-%m-%dT%H:%M:%SZ','now'),'Data lama (eksekusi belum dilacak)',0 WHERE EXISTS(SELECT 1 FROM mentions) OR EXISTS(SELECT 1 FROM collection_runs);
INSERT INTO execution_mentions SELECT 'legacy',id FROM mentions;
UPDATE collection_runs SET execution_id='legacy',data=json_set(data,'$.execution_id','legacy');
