-- A project's relay nodes go with the project. A platform node has a
-- NULL project_id, which a foreign key allows, so the node lifecycle is
-- no longer the only thing that removes one. Assignments cascade off
-- the node.
--
-- The certificate a node was issued is a certificates row named by the
-- node id, which no key can reach, so the project delete drops those
-- itself. Rows already left behind by deleted projects go here, or they
-- expire and the expiry sweep mails about them every day.

-- +goose Up
DELETE FROM relay_nodes
WHERE project_id IS NOT NULL
  AND project_id NOT IN (SELECT id FROM projects);

DELETE FROM certificates
WHERE scope = 'relay-node'
  AND name NOT IN (SELECT id::text FROM relay_nodes);

ALTER TABLE relay_nodes ADD CONSTRAINT relay_nodes_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE relay_nodes DROP CONSTRAINT relay_nodes_project_id_fkey;
