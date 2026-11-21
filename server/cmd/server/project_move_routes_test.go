package main

func init() {
	const reason = "source workspace owner only (checked before loading the project); neither a non-owner admin nor a plain project member can reach the project, and TestProjectMovePermissions covers who may call it"
	privateProjectRoutesNotProbed["POST /api/projects/{id}/move"] = reason
	privateProjectRoutesNotProbed["POST /api/projects/{id}/move/preview"] = reason
}
