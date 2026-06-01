package governance

import "testing"

func TestEvaluateAutomaticAction(t *testing.T) {
	action, ok := ActionByID("issue.status.remediate")
	if !ok {
		t.Fatal("missing action")
	}

	decision := Evaluate(action, Context{WorkspaceRole: "member"})
	if !decision.Allowed {
		t.Fatalf("expected automatic action to be allowed for member: %+v", decision)
	}
	if decision.RequiresApproval {
		t.Fatalf("automatic action should not require approval: %+v", decision)
	}
	if decision.Reason != "automatic_action_allowed" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateApprovalRequiredAction(t *testing.T) {
	action, ok := ActionByID("agent.create")
	if !ok {
		t.Fatal("missing action")
	}

	memberDecision := Evaluate(action, Context{WorkspaceRole: "member", Approved: true})
	if memberDecision.Allowed {
		t.Fatalf("plain member should not execute approval-required action: %+v", memberDecision)
	}
	if memberDecision.Reason != "requires_workspace_admin_or_owner" {
		t.Fatalf("member reason = %q", memberDecision.Reason)
	}

	adminWithoutApproval := Evaluate(action, Context{WorkspaceRole: "admin"})
	if adminWithoutApproval.Allowed {
		t.Fatalf("admin without approval should not execute approval-required action: %+v", adminWithoutApproval)
	}
	if !adminWithoutApproval.RequiresApproval || adminWithoutApproval.Reason != "approval_required" {
		t.Fatalf("admin without approval decision = %+v", adminWithoutApproval)
	}

	adminWithApproval := Evaluate(action, Context{WorkspaceRole: "admin", Approved: true})
	if !adminWithApproval.Allowed {
		t.Fatalf("admin with approval should execute action: %+v", adminWithApproval)
	}
	if adminWithApproval.RequiresApproval {
		t.Fatalf("approved action should no longer require approval: %+v", adminWithApproval)
	}
}

func TestEvaluateHardBoundary(t *testing.T) {
	action, ok := ActionByID("secret.reveal")
	if !ok {
		t.Fatal("missing action")
	}

	decision := Evaluate(action, Context{WorkspaceRole: "owner", Approved: true})
	if decision.Allowed {
		t.Fatalf("hard boundary action must stay denied: %+v", decision)
	}
	if decision.Reason != "hard_boundary" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestRoleTemplatesCoverRequiredDomains(t *testing.T) {
	templates := RoleTemplates()
	if len(templates) < 5 {
		t.Fatalf("expected role templates, got %d", len(templates))
	}

	var foundManagement bool
	for _, tmpl := range templates {
		if tmpl.ID == RoleManagementTeam {
			foundManagement = true
			if len(tmpl.Domains) != 6 {
				t.Fatalf("management team should cover all domains, got %v", tmpl.Domains)
			}
		}
	}
	if !foundManagement {
		t.Fatal("missing management team template")
	}
}
