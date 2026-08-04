import type { ReactNode } from "react"
import {
  AdvertisimentIcon,
  AiMagicIcon,
  Facebook02Icon,
  GoogleIcon,
  InstagramIcon,
  Linkedin01Icon,
  Mail01Icon,
  MoreHorizontalCircle01Icon,
  NewTwitterIcon,
  PodcastIcon,
  RedditIcon,
  UserMultiple02Icon,
  YoutubeIcon,
} from "@hugeicons/core-free-icons"
import { HugeiconsIcon } from "@hugeicons/react"
import { TargetIcon, GitBranchIcon, PaletteIcon, CodeIcon, RocketIcon, Settings2Icon, LayersIcon, UsersIcon, ArrowLeftRightIcon, SparklesIcon } from "lucide-react"

export type OnboardingStep = {
  id: string
  value: number
  label: string
  title: string
  description: string
  optional?: boolean
}

export type OnboardingChoice<TValue extends string = string> = {
  value: TValue
  label: string
  description: string
  icon?: ReactNode
  recommended?: boolean
}

export type RoleValue =
  | "product"
  | "engineering"
  | "design"
  | "developer"
  | "founder"
  | "operations"

export type TeamSizeValue =
  | "solo"
  | "small"
  | "team"
  | "department"
  | "midmarket"
  | "company"
  | "enterprise"
  | "global"

export type DiscoverySourceValue =
  | "google"
  | "linkedin"
  | "facebook"
  | "instagram"
  | "reddit"
  | "x"
  | "youtube"
  | "podcast"
  | "newsletter"
  | "friend"
  | "ai"
  | "outside"
  | "other"

export type GoalValue =
  | "roadmaps"
  | "sprints"
  | "projects"
  | "migration"
  | "explore"

export type InviteRoleValue = "guest" | "member" | "admin"

export type InviteRow = {
  id: string
  email: string
  role: InviteRoleValue
}

// Non-empty tuple so step lookups can fall back to index 0 under
// noUncheckedIndexedAccess.
export const ONBOARDING_STEPS: readonly [OnboardingStep, ...OnboardingStep[]] = [
  {
    id: "profile",
    value: 1,
    label: "Profile",
    title: "Set up your profile",
    description: "Add the details teammates will see across the workspace.",
  },
  {
    id: "role",
    value: 2,
    label: "Role",
    title: "Choose your role",
    description: "Starter views will match your daily work.",
  },
  {
    id: "source",
    value: 3,
    label: "Source",
    title: "How did you hear about us?",
    description:
      "Tell us where ReUI first showed up for you. This step is optional.",
    optional: true,
  },
  {
    id: "workspace",
    value: 4,
    label: "Workspace",
    title: "Create your workspace",
    description: "Name the shared space your team will use first.",
  },
  {
    id: "goals",
    value: 5,
    label: "Goals",
    title: "Choose your first goals",
    description: "Pick the workflows this workspace should support.",
  },
  {
    id: "invite",
    value: 6,
    label: "Invite",
    title: "Invite teammates",
    description: "Add the people who should join this workspace.",
    optional: true,
  },
]

export const DISCOVERY_SOURCE_OPTIONS: OnboardingChoice<DiscoverySourceValue>[] =
  [
    {
      value: "google",
      label: "Google",
      description: "Search or ads.",
      icon: (
        <HugeiconsIcon
          icon={GoogleIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "linkedin",
      label: "LinkedIn",
      description: "A professional post or share.",
      icon: (
        <HugeiconsIcon
          icon={Linkedin01Icon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "facebook",
      label: "Facebook",
      description: "A Facebook post or group mention.",
      icon: (
        <HugeiconsIcon
          icon={Facebook02Icon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "instagram",
      label: "Instagram",
      description: "A story, post, or creator share.",
      icon: (
        <HugeiconsIcon
          icon={InstagramIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "reddit",
      label: "Reddit",
      description: "A thread, review, or recommendation.",
      icon: (
        <HugeiconsIcon
          icon={RedditIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "x",
      label: "X.com",
      description: "A post or discussion on X.",
      icon: (
        <HugeiconsIcon
          icon={NewTwitterIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "youtube",
      label: "YouTube",
      description: "A demo, review, or walkthrough.",
      icon: (
        <HugeiconsIcon
          icon={YoutubeIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "podcast",
      label: "Podcast",
      description: "A show or interview.",
      icon: (
        <HugeiconsIcon
          icon={PodcastIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "newsletter",
      label: "Newsletter",
      description: "An email roundup.",
      icon: (
        <HugeiconsIcon
          icon={Mail01Icon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "friend",
      label: "Friend / coworker",
      description: "A teammate or peer recommended it.",
      icon: (
        <HugeiconsIcon
          icon={UserMultiple02Icon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "ai",
      label: "AI assistant",
      description: "Suggested during research.",
      icon: (
        <HugeiconsIcon
          icon={AiMagicIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "outside",
      label: "Billboard / outside",
      description: "Conference, billboard, or offline mention.",
      icon: (
        <HugeiconsIcon
          icon={AdvertisimentIcon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
    {
      value: "other",
      label: "Other",
      description: "Something else.",
      icon: (
        <HugeiconsIcon
          icon={MoreHorizontalCircle01Icon}
          className="size-4"
          aria-hidden="true"
        />
      ),
    },
  ]

export const ROLE_OPTIONS: OnboardingChoice<RoleValue>[] = [
  {
    value: "product",
    label: "Product Manager",
    description: "Plan roadmaps and align releases.",
    icon: (
      <TargetIcon aria-hidden="true" />
    ),
  },
  {
    value: "engineering",
    label: "Engineering Manager",
    description: "Coordinate cycles, dependencies, and reviews.",
    icon: (
      <GitBranchIcon aria-hidden="true" />
    ),
  },
  {
    value: "design",
    label: "Designer",
    description: "Shape specs, decisions, and handoff.",
    icon: (
      <PaletteIcon aria-hidden="true" />
    ),
  },
  {
    value: "developer",
    label: "Developer",
    description: "Track issues, reviews, and implementation.",
    icon: (
      <CodeIcon aria-hidden="true" />
    ),
    recommended: true,
  },
  {
    value: "founder",
    label: "Founder or Executive",
    description: "Keep launches and priorities in view.",
    icon: (
      <RocketIcon aria-hidden="true" />
    ),
  },
  {
    value: "operations",
    label: "Operations Manager",
    description: "Standardize ownership and handoffs.",
    icon: (
      <Settings2Icon aria-hidden="true" />
    ),
  },
]

export const TEAM_SIZE_OPTIONS: OnboardingChoice<TeamSizeValue>[] = [
  {
    value: "solo",
    label: "Just myself",
    description: "Personal setup.",
  },
  {
    value: "small",
    label: "2-10",
    description: "Small team.",
  },
  {
    value: "team",
    label: "11-50",
    description: "Growing team.",
    recommended: true,
  },
  {
    value: "department",
    label: "51-200",
    description: "Department.",
  },
  {
    value: "midmarket",
    label: "201-500",
    description: "Mid-market team.",
  },
  {
    value: "company",
    label: "501-1,000",
    description: "Company.",
  },
  {
    value: "enterprise",
    label: "1,001-5,000",
    description: "Enterprise.",
  },
  {
    value: "global",
    label: "5,000+",
    description: "Global organization.",
  },
]

export const GOAL_OPTIONS: OnboardingChoice<GoalValue>[] = [
  {
    value: "roadmaps",
    label: "Product roadmaps",
    description: "Connect goals, projects, and releases.",
    icon: (
      <LayersIcon aria-hidden="true" />
    ),
  },
  {
    value: "sprints",
    label: "Engineering sprints",
    description: "Keep issues, cycles, and reviews moving.",
    icon: (
      <CodeIcon aria-hidden="true" />
    ),
  },
  {
    value: "projects",
    label: "Cross-functional projects",
    description: "Give each team one source of truth.",
    icon: (
      <UsersIcon aria-hidden="true" />
    ),
  },
  {
    value: "migration",
    label: "Replace our current tool",
    description: "Use familiar fields and migration defaults.",
    icon: (
      <ArrowLeftRightIcon aria-hidden="true" />
    ),
  },
  {
    value: "explore",
    label: "Just exploring",
    description: "Start with sample data and lighter setup.",
    icon: (
      <SparklesIcon aria-hidden="true" />
    ),
  },
]

export const INVITE_ROLE_OPTIONS: OnboardingChoice<InviteRoleValue>[] = [
  {
    value: "guest",
    label: "Guest",
    description: "Can view invited projects.",
  },
  {
    value: "member",
    label: "Member",
    description: "Can create and update work.",
  },
  {
    value: "admin",
    label: "Admin",
    description: "Can manage workspace settings.",
  },
]

export const DEFAULT_INVITES: InviteRow[] = [
  {
    id: "invite-1",
    email: "maya@northstar.dev",
    role: "admin",
  },
  {
    id: "invite-2",
    email: "kai@northstar.dev",
    role: "member",
  },
]