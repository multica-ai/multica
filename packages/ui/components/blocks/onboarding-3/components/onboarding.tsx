"use client"

import { useState, type CSSProperties, type FormEvent } from "react"
import { IconStack } from "@multica/ui/components/reui/icon-stack"

import { cn } from "@multica/ui/lib/utils"
import { Button } from "@multica/ui/components/ui/button"
import { Checkbox } from "@multica/ui/components/ui/checkbox"
import {
  Combobox,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxLabel,
  ComboboxList,
  ComboboxSeparator,
} from "@multica/ui/components/ui/combobox"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@multica/ui/components/ui/field"
import { Input } from "@multica/ui/components/ui/input"
import { Item, ItemGroup, ItemMedia } from "@multica/ui/components/ui/item"
import {
  RadioGroup,
  RadioGroupItem,
} from "@multica/ui/components/ui/radio-group"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select"
import { Spinner } from "@multica/ui/components/ui/spinner"
import { Switch } from "@multica/ui/components/ui/switch"
import {
  DEFAULT_INVITES,
  DISCOVERY_SOURCE_OPTIONS,
  GOAL_OPTIONS,
  INVITE_ROLE_OPTIONS,
  ONBOARDING_STEPS,
  ROLE_OPTIONS,
  TEAM_SIZE_OPTIONS,
  type DiscoverySourceValue,
  type GoalValue,
  type InviteRoleValue,
  type InviteRow,
  type RoleValue,
  type TeamSizeValue,
} from "./data"
import { DotSphere } from "./dot-sphere"
import { ImageUploadField } from "./image-upload-field"
import { OnboardingLogo } from "./onboarding-logo"
import { OnboardingStepper } from "./onboarding-stepper"
import { CheckIcon, PlusIcon, CircleCheckIcon, ArrowLeftIcon, RocketIcon } from "lucide-react"

const TOTAL_STEPS = ONBOARDING_STEPS.length
const TIMEZONE_GROUPS = [
  {
    value: "Americas",
    items: [
      "(GMT-5) New York",
      "(GMT-8) Los Angeles",
      "(GMT-6) Chicago",
      "(GMT-5) Toronto",
      "(GMT-8) Vancouver",
      "(GMT-3) Sao Paulo",
    ],
  },
  {
    value: "Europe",
    items: [
      "(GMT+0) London",
      "(GMT+1) Paris",
      "(GMT+1) Berlin",
      "(GMT+1) Rome",
      "(GMT+1) Madrid",
      "(GMT+1) Amsterdam",
    ],
  },
  {
    value: "Asia/Pacific",
    items: [
      "(GMT+9) Tokyo",
      "(GMT+8) Shanghai",
      "(GMT+8) Singapore",
      "(GMT+4) Dubai",
      "(GMT+11) Sydney",
      "(GMT+9) Seoul",
    ],
  },
]

function getInviteRoleLabel(role: InviteRoleValue) {
  return (
    INVITE_ROLE_OPTIONS.find((option) => option.value === role)?.label ?? role
  )
}

function StepHeading({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <div className="flex max-w-[30rem] flex-col gap-1.5" aria-live="polite">
      <h1 className="text-foreground text-title-lg font-semibold text-balance sm:text-display-sm">
        {title}
      </h1>
      <p className="text-muted-foreground text-body text-pretty">
        {description}
      </p>
    </div>
  )
}

function ProfileStep({
  fullName,
  jobTitle,
  marketingOptIn,
  onFullNameChange,
  onJobTitleChange,
  onMarketingOptInChange,
}: {
  fullName: string
  jobTitle: string
  marketingOptIn: boolean
  onFullNameChange: (name: string) => void
  onJobTitleChange: (title: string) => void
  onMarketingOptInChange: (checked: boolean) => void
}) {
  return (
    <FieldSet>
      <FieldLegend className="sr-only">Profile details</FieldLegend>
      <FieldGroup className="gap-4">
        <Field>
          <ImageUploadField
            inputId="onboarding-3-profile-photo"
            alt="Sam Rivera"
            uploadLabel="Upload photo"
            replaceLabel="Replace photo"
            description="PNG or JPG, at least 400 x 400 px, up to 10 MB."
          />
        </Field>

        <FieldGroup className="gap-4">
          <Field className="gap-2">
            <FieldLabel htmlFor="onboarding-3-name">
              Full name <span className="text-destructive">*</span>
            </FieldLabel>
            <Input
              id="onboarding-3-name"
              value={fullName}
              onChange={(event) => onFullNameChange(event.target.value)}
              autoComplete="name"
            />
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="onboarding-3-title">Job title</FieldLabel>
            <Input
              id="onboarding-3-title"
              value={jobTitle}
              onChange={(event) => onJobTitleChange(event.target.value)}
              autoComplete="organization-title"
            />
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="onboarding-3-timezone">Timezone</FieldLabel>
            <Combobox items={TIMEZONE_GROUPS}>
              <ComboboxInput
                id="onboarding-3-timezone"
                placeholder="Select a timezone"
                className="w-full"
              />
              <ComboboxContent className="w-(--anchor-width) min-w-(--anchor-width)">
                <ComboboxEmpty>No timezones found.</ComboboxEmpty>
                <ComboboxList>
                  {(group) => (
                    <ComboboxGroup key={group.value} items={group.items}>
                      <ComboboxLabel>{group.value}</ComboboxLabel>
                      <ComboboxCollection>
                        {(item) => (
                          <ComboboxItem key={item} value={item}>
                            {item}
                          </ComboboxItem>
                        )}
                      </ComboboxCollection>
                      <ComboboxSeparator className="group-last/combobox-group:hidden" />
                    </ComboboxGroup>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
          </Field>
        </FieldGroup>

        <Field orientation="horizontal" className="gap-2.5">
          <Checkbox
            id="onboarding-3-marketing"
            checked={marketingOptIn}
            onCheckedChange={(checked) =>
              onMarketingOptInChange(checked === true)
            }
          />
          <FieldLabel
            htmlFor="onboarding-3-marketing"
            className="text-muted-foreground font-normal"
          >
            Send me product updates and workspace tips.
          </FieldLabel>
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

function RoleStep({
  role,
  onRoleChange,
}: {
  role: RoleValue
  onRoleChange: (role: RoleValue) => void
}) {
  return (
    <FieldSet className="gap-3">
      <FieldLegend variant="label">Select one</FieldLegend>
      <RadioGroup
        value={role}
        onValueChange={(value) => onRoleChange(value as RoleValue)}
        aria-label="Select your role"
      >
        <ItemGroup className="gap-2">
          {ROLE_OPTIONS.map((option) => {
            const fieldId = `onboarding-3-role-${option.value}`

            return (
              <Item key={option.value} variant="outline" size="sm">
                <Field orientation="horizontal" className="w-full gap-3">
                  <RadioGroupItem id={fieldId} value={option.value} />
                  <FieldLabel
                    htmlFor={fieldId}
                    className="items-center leading-none"
                  >
                    <span className="truncate font-medium">{option.label}</span>
                  </FieldLabel>
                </Field>
              </Item>
            )
          })}
        </ItemGroup>
      </RadioGroup>
    </FieldSet>
  )
}

function SourceStep({
  source,
  otherSource,
  onSourceChange,
  onOtherSourceChange,
}: {
  source: DiscoverySourceValue | ""
  otherSource: string
  onSourceChange: (source: DiscoverySourceValue) => void
  onOtherSourceChange: (source: string) => void
}) {
  return (
    <FieldSet className="gap-4">
      <FieldLegend className="sr-only">Discovery source</FieldLegend>
      <FieldGroup
        className="flex-row flex-wrap gap-2"
        aria-label="How did you hear about us?"
      >
        {DISCOVERY_SOURCE_OPTIONS.map((option) => {
          const selected = option.value === source

          return (
            <Button
              key={option.value}
              type="button"
              variant="outline"
              size="lg"
              aria-pressed={selected}
              className={cn(selected && "border-primary/30 bg-primary/5")}
              onClick={() => onSourceChange(option.value)}
            >
              <span
                className={cn(
                  "text-muted-foreground flex shrink-0 items-center [&_svg]:size-4",
                  selected && "text-primary"
                )}
              >
                {option.icon}
              </span>
              <span>{option.label}</span>
              {selected ? (
                <CheckIcon className="size-4 shrink-0" aria-hidden="true" />
              ) : null}
            </Button>
          )
        })}
      </FieldGroup>

      {source === "other" ? (
        <Field className="max-w-sm gap-2">
          <FieldLabel htmlFor="onboarding-3-source-other-text">
            Where did you hear about ReUI?
          </FieldLabel>
          <Input
            id="onboarding-3-source-other-text"
            value={otherSource}
            onChange={(event) => onOtherSourceChange(event.target.value)}
            placeholder="Type the source"
            autoComplete="off"
          />
        </Field>
      ) : null}
    </FieldSet>
  )
}

function WorkspaceStep({
  workspaceName,
  workspaceSlug,
  teamSize,
  onWorkspaceNameChange,
  onWorkspaceSlugChange,
  onTeamSizeChange,
}: {
  workspaceName: string
  workspaceSlug: string
  teamSize: TeamSizeValue
  onWorkspaceNameChange: (value: string) => void
  onWorkspaceSlugChange: (value: string) => void
  onTeamSizeChange: (value: TeamSizeValue) => void
}) {
  return (
    <FieldSet>
      <FieldLegend className="sr-only">Workspace setup</FieldLegend>
      <FieldGroup className="gap-5">
        <FieldGroup className="gap-4">
          <Field className="gap-2">
            <FieldLabel htmlFor="onboarding-3-workspace">
              Workspace name <span className="text-destructive">*</span>
            </FieldLabel>
            <Input
              id="onboarding-3-workspace"
              value={workspaceName}
              onChange={(event) => onWorkspaceNameChange(event.target.value)}
              autoComplete="organization"
            />
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="onboarding-3-url">
              Workspace URL <span className="text-destructive">*</span>
            </FieldLabel>
            <Input
              id="onboarding-3-url"
              value={workspaceSlug}
              onChange={(event) => onWorkspaceSlugChange(event.target.value)}
              autoComplete="off"
            />
            <FieldDescription>
              Your workspace will open at app.reui.dev/
              {workspaceSlug.trim() || "workspace"}.
            </FieldDescription>
          </Field>
        </FieldGroup>

        <FieldSet className="gap-5">
          <FieldLegend variant="label" className="mb-4">
            How many people will use this workspace?
          </FieldLegend>
          <FieldGroup
            className="flex-row flex-wrap gap-2"
            aria-label="Workspace team size"
          >
            {TEAM_SIZE_OPTIONS.map((option) => {
              const selected = option.value === teamSize

              return (
                <Button
                  key={option.value}
                  type="button"
                  variant="outline"
                  aria-pressed={selected}
                  className={cn(
                    "relative overflow-visible px-5",
                    selected && "border-primary/30 bg-primary/5"
                  )}
                  onClick={() => onTeamSizeChange(option.value)}
                >
                  <span>{option.label}</span>
                  {selected ? (
                    <Item
                      render={<span />}
                      className="bg-primary text-primary-foreground ring-background absolute -top-2 -right-2 flex size-5 items-center justify-center rounded-full p-0 ring-2"
                    >
                      <ItemMedia variant="icon" className="size-auto">
                        <CheckIcon className="size-3" aria-hidden="true" />
                      </ItemMedia>
                    </Item>
                  ) : null}
                </Button>
              )
            })}
          </FieldGroup>
        </FieldSet>
      </FieldGroup>
    </FieldSet>
  )
}

function GoalsStep({
  goals,
  onGoalToggle,
}: {
  goals: GoalValue[]
  onGoalToggle: (goal: GoalValue, checked: boolean) => void
}) {
  return (
    <FieldSet className="gap-3">
      <FieldLegend variant="label">Select one or more</FieldLegend>
      <ItemGroup className="gap-2">
        {GOAL_OPTIONS.map((option) => {
          const selected = goals.includes(option.value)
          const fieldId = `onboarding-3-goal-${option.value}`

          return (
            <Item key={option.value} variant="outline" size="sm">
              <Field orientation="horizontal" className="w-full gap-3">
                <Checkbox
                  id={fieldId}
                  checked={selected}
                  onCheckedChange={(checked) =>
                    onGoalToggle(option.value, checked === true)
                  }
                />
                <FieldLabel
                  htmlFor={fieldId}
                  className="items-center leading-none"
                >
                  <span className="truncate font-medium">{option.label}</span>
                </FieldLabel>
              </Field>
            </Item>
          )
        })}
      </ItemGroup>
    </FieldSet>
  )
}

function InviteRoleSelect({
  id,
  value,
  onValueChange,
}: {
  id: string
  value: InviteRoleValue
  onValueChange: (value: InviteRoleValue) => void
}) {
  return (
    <Select
      items={INVITE_ROLE_OPTIONS}
      value={value}
      onValueChange={(nextValue) =>
        nextValue && onValueChange(nextValue as InviteRoleValue)
      }
    >
      <SelectTrigger id={id} className="w-full">
        <SelectValue>
          {(item: InviteRoleValue) => getInviteRoleLabel(item)}
        </SelectValue>
      </SelectTrigger>
      <SelectContent
        align="start"
        alignItemWithTrigger={false}
        className="w-64 min-w-(--anchor-width)"
      >
        <SelectGroup>
          {INVITE_ROLE_OPTIONS.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              <span className="flex min-w-0 flex-col items-start gap-px">
                <span className="font-medium">{option.label}</span>
                <small className="text-muted-foreground line-clamp-1 text-caption">
                  {option.description}
                </small>
              </span>
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

function InviteStep({
  invites,
  sendInviteDigest,
  onAddInvite,
  onInviteEmailChange,
  onInviteRoleChange,
  onSendInviteDigestChange,
}: {
  invites: InviteRow[]
  sendInviteDigest: boolean
  onAddInvite: () => void
  onInviteEmailChange: (inviteId: string, email: string) => void
  onInviteRoleChange: (inviteId: string, role: InviteRoleValue) => void
  onSendInviteDigestChange: (checked: boolean) => void
}) {
  return (
    <FieldSet>
      <FieldLegend className="sr-only">Invite teammates</FieldLegend>
      <FieldGroup className="gap-5">
        <div className="grid gap-3">
          <div className="text-muted-foreground hidden grid-cols-[minmax(0,1fr)_9rem] gap-3 px-1 text-caption font-medium sm:grid">
            <span>Email</span>
            <span>Role</span>
          </div>
          {invites.map((invite, index) => {
            const emailId = `onboarding-3-invite-email-${invite.id}`
            const roleId = `onboarding-3-invite-role-${invite.id}`

            return (
              <div
                key={invite.id}
                className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_9rem]"
              >
                <Field>
                  <FieldLabel htmlFor={emailId} className="sr-only">
                    Invitee {index + 1} email
                  </FieldLabel>
                  <Input
                    id={emailId}
                    type="email"
                    value={invite.email}
                    onChange={(event) =>
                      onInviteEmailChange(invite.id, event.target.value)
                    }
                    placeholder="teammate@company.com"
                    autoComplete="email"
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor={roleId} className="sr-only">
                    Invitee {index + 1} role
                  </FieldLabel>
                  <InviteRoleSelect
                    id={roleId}
                    value={invite.role}
                    onValueChange={(role) =>
                      onInviteRoleChange(invite.id, role)
                    }
                  />
                </Field>
              </div>
            )
          })}
          <div className="flex justify-end">
            <Button
              type="button"
              variant="link"
              className="h-auto w-fit px-1"
              onClick={onAddInvite}
            >
              <PlusIcon aria-hidden="true" data-icon="inline-start" />
              Add another
            </Button>
          </div>
        </div>

        <Field orientation="horizontal" className="gap-3">
          <FieldContent className="gap-0.5">
            <FieldLabel htmlFor="onboarding-3-send-digest">
              Send invitation summary
            </FieldLabel>
            <FieldDescription>Include workspace details.</FieldDescription>
          </FieldContent>
          <Switch
            id="onboarding-3-send-digest"
            checked={sendInviteDigest}
            onCheckedChange={onSendInviteDigestChange}
          />
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

function SuccessStep({
  workspaceName,
  onReviewSetup,
}: {
  workspaceName: string
  onReviewSetup: () => void
}) {
  const displayName = workspaceName.trim() || "Workspace"

  return (
    <div className="mx-auto flex w-full max-w-sm flex-1 flex-col">
      <div className="flex flex-1 flex-col justify-center">
        <div
          className="mx-auto flex h-28 w-full items-center justify-center"
          aria-hidden="true"
        >
          <IconStack
            className="text-primary h-24 w-22"
            style={
              {
                "--icon-stack-content-x": "70%",
                "--icon-stack-content-y": "57%",
              } as CSSProperties
            }
          >
            <CircleCheckIcon className="text-primary size-5" strokeWidth="1.8" aria-hidden="true" />
          </IconStack>
        </div>

        <div className="mx-auto mt-3 max-w-sm text-center">
          <h1 className="text-foreground text-display-sm font-semibold tracking-tight">
            {displayName} is ready
          </h1>
          <p className="text-muted-foreground mt-2 text-body leading-6">
            Your workspace is set up and ready for the first project.
          </p>
        </div>
      </div>

      <div className="mt-auto flex flex-col gap-2 pt-8">
        <Button type="button" className="w-full">
          Open {displayName}
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="w-full"
          onClick={onReviewSetup}
        >
          Review setup
        </Button>
      </div>
    </div>
  )
}

function OnboardingSidebar({
  currentStep,
  isComplete,
  canGoBack,
  onBack,
  onStepChange,
}: {
  currentStep: number
  isComplete: boolean
  canGoBack: boolean
  onBack: () => void
  onStepChange: (step: number) => void
}) {
  return (
    <aside className="relative z-10 flex min-h-[25rem] w-full shrink-0 px-4 py-4 sm:px-5 sm:py-5 lg:min-h-svh lg:w-[22rem] lg:px-5 lg:py-5">
      <div className="dark bg-background text-foreground ring-border relative isolate flex min-h-full w-full overflow-hidden rounded-2xl px-5 py-5 ring-1">
        <div
          aria-hidden="true"
          className="bg-background pointer-events-none absolute inset-0 overflow-hidden"
        >
          <DotSphere
            dotGap={19}
            motion="wave"
            sphereCount={5}
            sphereRadius="20%"
            dotRadiusMax={1.9}
            speed={0.4}
          />
        </div>

        <div className="relative flex min-h-full w-full flex-col">
          <header className="flex min-h-9 items-center justify-between gap-3">
            <OnboardingLogo className="[&_span]:text-slate-50 [&>div]:bg-slate-50 [&>div]:text-slate-950" />
            {canGoBack ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="text-white/65 hover:bg-white/10 hover:text-white"
                onClick={onBack}
                aria-label="Back to previous step"
              >
                <ArrowLeftIcon aria-hidden="true" />
              </Button>
            ) : null}
          </header>

          <div className="flex flex-1 items-center justify-center px-2 py-10 sm:px-3 sm:py-12 lg:px-2 lg:py-16">
            <OnboardingStepper
              currentStep={currentStep}
              isComplete={isComplete}
              onStepChange={onStepChange}
              steps={ONBOARDING_STEPS}
            />
          </div>

          <footer className="flex min-h-8 shrink-0 items-end justify-between gap-4 text-caption">
            <button
              type="button"
              className="rounded-sm text-left text-white/60 hover:text-white focus-visible:ring-2 focus-visible:ring-white/45 focus-visible:outline-none"
            >
              Terms of Service
            </button>
            <button
              type="button"
              className="rounded-sm text-right text-white/60 hover:text-white focus-visible:ring-2 focus-visible:ring-white/45 focus-visible:outline-none"
            >
              Help Center
            </button>
          </footer>
        </div>
      </div>
    </aside>
  )
}

export function Onboarding() {
  const [currentStep, setCurrentStep] = useState(1)
  const [fullName, setFullName] = useState("Sam Rivera")
  const [jobTitle, setJobTitle] = useState("Product Lead")
  const [marketingOptIn, setMarketingOptIn] = useState(true)
  const [role, setRole] = useState<RoleValue>("developer")
  const [discoverySource, setDiscoverySource] =
    useState<DiscoverySourceValue>("linkedin")
  const [discoveryOther, setDiscoveryOther] = useState("")
  const [workspaceName, setWorkspaceName] = useState("ReUI Labs")
  const [workspaceSlug, setWorkspaceSlug] = useState("northstar")
  const [teamSize, setTeamSize] = useState<TeamSizeValue>("team")
  const [goals, setGoals] = useState<GoalValue[]>(["roadmaps", "sprints"])
  const [invites, setInvites] = useState<InviteRow[]>(DEFAULT_INVITES)
  const [sendInviteDigest, setSendInviteDigest] = useState(true)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isComplete, setIsComplete] = useState(false)

  const currentStepMeta = ONBOARDING_STEPS[currentStep - 1] ?? ONBOARDING_STEPS[0]
  const isFinalStep = currentStep === TOTAL_STEPS
  const canSkip = currentStep > 1 && currentStep < TOTAL_STEPS
  const canContinue =
    (currentStepMeta.id !== "profile" || fullName.trim().length > 0) &&
    (currentStepMeta.id !== "workspace" ||
      (workspaceName.trim().length > 0 && workspaceSlug.trim().length > 0)) &&
    (currentStepMeta.id !== "goals" || goals.length > 0)

  function goToStep(step: number) {
    setIsComplete(false)
    setCurrentStep(Math.min(Math.max(step, 1), TOTAL_STEPS))
  }

  function handleGoalToggle(goal: GoalValue, checked: boolean) {
    setGoals((currentGoals) => {
      if (checked) {
        return currentGoals.includes(goal)
          ? currentGoals
          : [...currentGoals, goal]
      }

      return currentGoals.filter((currentGoal) => currentGoal !== goal)
    })
  }

  function handleAddInvite() {
    setInvites((currentInvites) => [
      ...currentInvites,
      {
        id: `invite-${currentInvites.length + 1}`,
        email: "",
        role: "member",
      },
    ])
  }

  function handleInviteEmailChange(inviteId: string, email: string) {
    setInvites((currentInvites) =>
      currentInvites.map((invite) =>
        invite.id === inviteId ? { ...invite, email } : invite
      )
    )
  }

  function handleInviteRoleChange(inviteId: string, nextRole: InviteRoleValue) {
    setInvites((currentInvites) =>
      currentInvites.map((invite) =>
        invite.id === inviteId ? { ...invite, role: nextRole } : invite
      )
    )
  }

  function completeOnboarding() {
    if (isSubmitting) {
      return
    }

    setIsSubmitting(true)

    window.setTimeout(() => {
      setIsSubmitting(false)
      setIsComplete(true)
    }, 700)
  }

  function handleSkip() {
    if (!canSkip) {
      return
    }

    goToStep(currentStep + 1)
  }

  function handleStepNavigation(step: number) {
    if (isComplete || step <= currentStep) {
      goToStep(step)
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()

    if (!canContinue) {
      return
    }

    if (!isFinalStep) {
      goToStep(currentStep + 1)
      return
    }

    completeOnboarding()
  }

  return (
    <main className="text-foreground bg-background relative isolate flex min-h-svh w-full flex-col lg:flex-row">
      <OnboardingSidebar
        currentStep={currentStep}
        isComplete={isComplete}
        canGoBack={!isComplete && currentStep > 1}
        onBack={() => goToStep(currentStep - 1)}
        onStepChange={handleStepNavigation}
      />

      <section className="relative z-10 flex min-w-0 flex-1 items-center justify-center px-6 py-8 sm:px-10 lg:px-14 lg:py-10">
        <div className="flex min-h-[34rem] w-full max-w-[28rem] flex-col sm:min-h-[36rem]">
          {isComplete ? (
            <SuccessStep
              workspaceName={workspaceName}
              onReviewSetup={() => {
                setIsComplete(false)
                setCurrentStep(TOTAL_STEPS)
              }}
            />
          ) : (
            <form
              className="flex min-h-[inherit] flex-col"
              onSubmit={handleSubmit}
            >
              <div className="flex flex-col gap-8 pt-2 sm:pt-6">
                <StepHeading
                  title={currentStepMeta.title}
                  description={currentStepMeta.description}
                />

                {currentStepMeta.id === "profile" ? (
                  <ProfileStep
                    fullName={fullName}
                    jobTitle={jobTitle}
                    marketingOptIn={marketingOptIn}
                    onFullNameChange={setFullName}
                    onJobTitleChange={setJobTitle}
                    onMarketingOptInChange={setMarketingOptIn}
                  />
                ) : null}

                {currentStepMeta.id === "role" ? (
                  <RoleStep role={role} onRoleChange={setRole} />
                ) : null}

                {currentStepMeta.id === "source" ? (
                  <SourceStep
                    source={discoverySource}
                    otherSource={discoveryOther}
                    onSourceChange={setDiscoverySource}
                    onOtherSourceChange={setDiscoveryOther}
                  />
                ) : null}

                {currentStepMeta.id === "workspace" ? (
                  <WorkspaceStep
                    workspaceName={workspaceName}
                    workspaceSlug={workspaceSlug}
                    teamSize={teamSize}
                    onWorkspaceNameChange={setWorkspaceName}
                    onWorkspaceSlugChange={setWorkspaceSlug}
                    onTeamSizeChange={setTeamSize}
                  />
                ) : null}

                {currentStepMeta.id === "goals" ? (
                  <GoalsStep goals={goals} onGoalToggle={handleGoalToggle} />
                ) : null}

                {currentStepMeta.id === "invite" ? (
                  <InviteStep
                    invites={invites}
                    sendInviteDigest={sendInviteDigest}
                    onAddInvite={handleAddInvite}
                    onInviteEmailChange={handleInviteEmailChange}
                    onInviteRoleChange={handleInviteRoleChange}
                    onSendInviteDigestChange={setSendInviteDigest}
                  />
                ) : null}
              </div>

              <div className="mt-auto flex flex-col gap-2 pt-10 pb-2">
                <Button
                  type="submit"
                  className="w-full"
                  disabled={!canContinue || isSubmitting}
                >
                  {isSubmitting ? (
                    <Spinner data-icon="inline-start" aria-hidden="true" />
                  ) : isFinalStep ? (
                    <RocketIcon aria-hidden="true" data-icon="inline-start" />
                  ) : null}
                  {isFinalStep ? "Create workspace" : "Continue"}
                </Button>

                {canSkip ? (
                  <Button
                    type="button"
                    variant="ghost"
                    className="w-full"
                    disabled={isSubmitting}
                    onClick={handleSkip}
                  >
                    Skip
                  </Button>
                ) : null}
              </div>
            </form>
          )}
        </div>
      </section>
    </main>
  )
}