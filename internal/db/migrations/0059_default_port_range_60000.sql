-- New nodes listen on 10001-60000. Existing rows still on the old default
-- follow so operators who never customized the range get the wider pool.
UPDATE nodes SET port_range = '10001-60000' WHERE port_range = '10001-20000';
