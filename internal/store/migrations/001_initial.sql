CREATE TABLE users (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 username TEXT NOT NULL COLLATE NOCASE UNIQUE,
 email TEXT NOT NULL DEFAULT '',
 password_hash TEXT NOT NULL,
 role TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('user','admin')),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','banned')),
 created_at TEXT NOT NULL,
 last_login_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at INTEGER NOT NULL,
 user_agent TEXT NOT NULL DEFAULT '',
 ip TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE problems (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 title TEXT NOT NULL,
 difficulty TEXT NOT NULL CHECK(difficulty IN ('easy','medium','hard')),
 description TEXT NOT NULL DEFAULT '',
 input_format TEXT NOT NULL DEFAULT '',
 output_format TEXT NOT NULL DEFAULT '',
 hint TEXT NOT NULL DEFAULT '',
 time_limit_ms INTEGER NOT NULL CHECK(time_limit_ms>0),
 memory_limit_mb INTEGER NOT NULL CHECK(memory_limit_mb>0),
 checker TEXT NOT NULL DEFAULT 'trim' CHECK(checker IN ('exact','trim','float')),
 float_eps REAL NOT NULL DEFAULT 0.000001 CHECK(float_eps>0),
 is_public INTEGER NOT NULL DEFAULT 0 CHECK(is_public IN (0,1)),
 created_by INTEGER NOT NULL DEFAULT 0,
 submit_count INTEGER NOT NULL DEFAULT 0 CHECK(submit_count>=0),
 accepted_count INTEGER NOT NULL DEFAULT 0 CHECK(accepted_count>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX problems_public ON problems(is_public,id);
CREATE TABLE problem_tags (
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 tag TEXT NOT NULL,
 PRIMARY KEY(problem_id,tag)
);
CREATE INDEX tags_name ON problem_tags(tag,problem_id);
CREATE TABLE testcases (
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 idx INTEGER NOT NULL,
 input TEXT NOT NULL,
 expected TEXT NOT NULL,
 is_sample INTEGER NOT NULL CHECK(is_sample IN (0,1)),
 explanation TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(problem_id,idx)
);
CREATE TABLE problem_templates (
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 language TEXT NOT NULL,
 code TEXT NOT NULL,
 PRIMARY KEY(problem_id,language)
);
CREATE TABLE problem_lang_limits (
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 language TEXT NOT NULL,
 time_limit_ms INTEGER CHECK(time_limit_ms IS NULL OR time_limit_ms>0),
 memory_limit_mb INTEGER CHECK(memory_limit_mb IS NULL OR memory_limit_mb>0),
 PRIMARY KEY(problem_id,language)
);
CREATE TABLE problem_solutions (
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 language TEXT NOT NULL,
 code TEXT NOT NULL,
 PRIMARY KEY(problem_id,language)
);
-- problem_id intentionally has no foreign key: a submission remains meaningful
-- after deletion; its immutable problem title and numeric id are retained.
CREATE TABLE submissions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 problem_id INTEGER NOT NULL,
 problem_title TEXT NOT NULL,
 user_id INTEGER NOT NULL REFERENCES users(id),
 language TEXT NOT NULL,
 code TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','judging','accepted','wrong_answer','time_limit_exceeded','memory_limit_exceeded','runtime_error','compile_error','output_limit_exceeded','system_error')),
 time_us INTEGER NOT NULL DEFAULT 0 CHECK(time_us>=0),
 memory_kb INTEGER NOT NULL DEFAULT 0 CHECK(memory_kb>=0),
 passed INTEGER NOT NULL DEFAULT 0 CHECK(passed>=0),
 total INTEGER NOT NULL DEFAULT 0 CHECK(total>=passed),
 message TEXT NOT NULL DEFAULT '',
 case_results TEXT NOT NULL DEFAULT '[]',
 created_at TEXT NOT NULL,
 judged_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX submissions_user ON submissions(user_id,id DESC);
CREATE INDEX submissions_problem ON submissions(problem_id,id DESC);
CREATE INDEX submissions_pending ON submissions(status,user_id);
CREATE TABLE user_problem_status (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
 status TEXT NOT NULL CHECK(status IN ('attempted','solved')),
 updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id,problem_id)
);
