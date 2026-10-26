-- name: GetAIDOwnerBinding :one
SELECT * FROM aido_owner_binding WHERE agent_id=$1;

-- name: CreateAIDOwnerBinding :one
INSERT INTO aido_owner_binding(agent_id,runtime_id) VALUES($1,$2) RETURNING *;
