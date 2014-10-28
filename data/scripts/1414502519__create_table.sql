--  create table --
-- @DO sql script --
CREATE TABLE test (id int, value varchar);

-- @UNDO sql script --
DROP TABLE test;

