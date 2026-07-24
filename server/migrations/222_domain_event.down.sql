-- Reverses 222_domain_event.up.sql. Dropping the table also drops the
-- sequence it OWNS; the explicit DROP SEQUENCE is a defensive no-op in case
-- the ownership link is ever severed.
DROP TABLE IF EXISTS domain_event;
DROP SEQUENCE IF EXISTS domain_event_seq;
