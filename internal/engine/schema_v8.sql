-- Migration 8. A checkbox is the to-do tag: a node is a to-do if and only
-- if it carries a tag with the to-do trait. Checkboxes set on other nodes
-- while the two were separate are dropped; the nodes stay.
UPDATE nodes SET status = NULL WHERE status IS NOT NULL
  AND seq NOT IN (SELECT nt.node_seq FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE t.todo = 1);
