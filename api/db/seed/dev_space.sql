-- The stand-in tenant until login (Step 5). Prints its id for DEV_SPACE_ID.
INSERT INTO spaces (name, slug) VALUES ('Dev space', 'dev')
ON CONFLICT (slug) DO NOTHING;

SELECT id FROM spaces WHERE slug = 'dev';
