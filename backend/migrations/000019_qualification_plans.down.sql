DROP TABLE IF EXISTS qualification_plan_suites;
DROP TRIGGER IF EXISTS qualification_plans_immutable ON qualification_plans;
DROP FUNCTION IF EXISTS reject_qualification_plan_mutation();
DROP TABLE IF EXISTS qualification_plans;
