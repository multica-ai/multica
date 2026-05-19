package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel/binding"
	chcommand "github.com/multica-ai/multica/server/internal/channel/command"
	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/facadeimpl"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
	chturn "github.com/multica-ai/multica/server/internal/channel/turn"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type channelPipelineOptions struct {
	Storage        storage.Storage
	FileDownloader port.FileDownloader
	Gateway        port.ChannelGateway
	Observer       inbound.Observer
	ChannelTurn    chturn.AgentClient
	TaskService    *service.TaskService
}

type channelInboundRuntimeComponents struct {
	PrePipeline        *inbound.Pipeline
	PostPipeline       *inbound.Pipeline
	RuleResolvers      []chcommand.Resolver
	ChannelTurn        chturn.AgentClient
	DispatchStore      inbound.DispatchCompletionStore
	ConversationStore  channelconversation.Store
	ContextMaxEntities int
}

func newChannelInboundRuntimeComponents(pool *pgxpool.Pool, opts ...channelPipelineOptions) channelInboundRuntimeComponents {
	queries := db.New(pool)
	issueSvc := facadeimpl.NewIssueService(pool)
	issueDigestSvc := facadeimpl.NewIssueDigestService(pool)
	commentSvc := facadeimpl.NewCommentService(queries, issueSvc)
	bindings := inbound.NewDBChatBindingLookup(pool)
	userResolver := inbound.NewDBUserInfoResolver(pool)
	issuer := binding.NewTokenIssuer(queries)
	conversationStore := channelconversation.NewDBStore(pool)

	var opt channelPipelineOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	replySink := inbound.NewGatewayReplySink(opt.Gateway, inbound.WithGatewayReplyConversationStore(conversationStore))

	ruleResolvers := []chcommand.Resolver{
		chcommand.NewRuleResolver(chcommand.NewRuleMatcher()),
	}
	channelTurn := opt.ChannelTurn
	if channelTurn == nil && opt.TaskService != nil {
		channelTurn = facadeimpl.NewTaskBackedChannelTurnClient(queries, opt.TaskService, bindings)
	}

	pre := inbound.NewPipeline(
		inbound.NewNormalizeStep(),
		inbound.NewUserIdentityBindStep(pool, replySink, issuer),
		inbound.NewChatBindCommandStep(opt.Gateway, replySink, issuer, bindings),
		inbound.NewDirectChatPolicyStep(replySink),
		inbound.NewChatSettingsFilterStep(inbound.NewDBInboundEventStore(pool)),
		inbound.NewSlashStep(inbound.SlashConfig{ReplySink: replySink}),
	)
	pre.SetObserver(opt.Observer)

	postSteps := []inbound.Step{
		inbound.NewAuthzStep(inbound.AuthzConfig{
			Store:        bindings,
			ReplySink:    replySink,
			SendReplies:  true,
			RejectAsSkip: true,
		}),
	}
	if opt.Storage != nil && (opt.FileDownloader != nil || opt.Gateway != nil) {
		postSteps = append(postSteps, inbound.NewAttachmentStep(inbound.AttachmentConfig{
			Storage:           opt.Storage,
			AttachmentQuerier: facade.NewAttachmentFacade(facadeimpl.NewAttachmentService(queries)),
			FileDownloader:    opt.FileDownloader,
			Gateway:           opt.Gateway,
			ReplySink:         replySink,
			ChatBinding:       bindings,
			UserResolver:      userResolver,
			IssueFacade:       facade.NewIssueFacade(issueSvc),
		}))
	} else if len(opts) > 0 && (opt.Storage != nil || opt.FileDownloader != nil) {
		slog.Info("channel attachment step disabled: storage or file downloader is not configured")
	}
	postSteps = append(postSteps,
		inbound.NewDispatchStep(inbound.DispatchConfig{
			IssueFacade:       facade.NewIssueFacade(issueSvc),
			IssueDigestFacade: facade.NewIssueDigestFacade(issueDigestSvc),
			CommentFacade:     facade.NewCommentFacade(commentSvc),
			ReplySink:         replySink,
			ChatBinding:       bindings,
			UserResolver:      userResolver,
			ProjectValidator:  inbound.NewDBProjectWorkspaceValidator(pool),
			DispatchStore:     inbound.NewDBDispatchCompletionStore(pool),
			ProposalStore:     inbound.NewDBActionProposalStore(pool),
		}),
	)
	post := inbound.NewPipeline(postSteps...)
	post.SetObserver(opt.Observer)

	return channelInboundRuntimeComponents{
		PrePipeline:       pre,
		PostPipeline:      post,
		RuleResolvers:     ruleResolvers,
		ChannelTurn:       channelTurn,
		DispatchStore:     inbound.NewDBDispatchCompletionStore(pool),
		ConversationStore: conversationStore,
	}
}
