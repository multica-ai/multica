package handler

const (
	commentMentionPolicyUnrestricted             = "unrestricted"
	commentMentionPolicyCreatorOnlyForNonCreator = "creator_only_for_non_creator"
)

func validCommentMentionPolicy(policy string) bool {
	return policy == commentMentionPolicyUnrestricted || policy == commentMentionPolicyCreatorOnlyForNonCreator
}
