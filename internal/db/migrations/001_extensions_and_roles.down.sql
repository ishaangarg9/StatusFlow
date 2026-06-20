DROP FUNCTION IF EXISTS current_org();
DROP TYPE IF EXISTS org_role;
-- The role is intentionally not dropped: it may still own objects in other schemas.
-- To remove manually: REASSIGN OWNED BY app_user TO postgres; DROP OWNED BY app_user; DROP ROLE app_user;
-- Extensions are left in place.
