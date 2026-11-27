-- Personal profile: an optional display name and a user-uploaded avatar.
-- display_name overrides the directory / SSO name in the UI when non-empty.
-- user_avatars holds one cropped JPEG/PNG per user; version is the cache key
-- carried in the avatar URL, so replacing an avatar never serves a stale one.

SET @display_name_ddl = IF(
 (SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='users' AND column_name='display_name')=0,
 'ALTER TABLE users ADD COLUMN display_name VARCHAR(128) NOT NULL DEFAULT ''''',
 'SELECT 1');
PREPARE display_name_stmt FROM @display_name_ddl;
EXECUTE display_name_stmt;
DEALLOCATE PREPARE display_name_stmt;

CREATE TABLE IF NOT EXISTS user_avatars (
  user_id BIGINT NOT NULL,
  content_type VARCHAR(64) NOT NULL,
  data MEDIUMBLOB NOT NULL,
  version BIGINT NOT NULL,
  updated_at DATETIME(3) NULL,
  PRIMARY KEY (user_id)
);
