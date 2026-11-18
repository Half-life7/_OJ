ALTER TABLE testcases ADD COLUMN sample_only INTEGER NOT NULL DEFAULT 0
 CHECK(sample_only IN (0,1) AND (sample_only = 0 OR is_sample = 1));
