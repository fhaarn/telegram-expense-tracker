ALTER TABLE users ADD COLUMN is_smoker BOOLEAN;
INSERT INTO categories(user_id,display_name,normalized_name)
 SELECT u.id,c.name,lower(c.name) FROM users u
 CROSS JOIN (VALUES ('Coffee & drinks'),('Smoking & vaping')) c(name)
 WHERE u.status='active' ON CONFLICT(user_id,normalized_name) DO NOTHING;
