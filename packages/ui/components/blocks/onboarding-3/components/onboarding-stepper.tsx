import {
  Stepper,
  StepperDescription,
  StepperIndicator,
  StepperItem,
  StepperNav,
  StepperSeparator,
  StepperTitle,
  StepperTrigger,
} from "@multica/ui/components/reui/stepper"
import type { OnboardingStep } from "./data"
import { CheckIcon } from "lucide-react"

const SIDEBAR_STEP_DESCRIPTIONS: Record<string, string> = {
  profile: "Identity and timezone.",
  role: "Role and use case.",
  source: "Discovery source.",
  workspace: "Workspace details.",
  goals: "First workflows.",
  invite: "Optional teammates.",
}

export function OnboardingStepper({
  currentStep,
  isComplete,
  onStepChange,
  steps,
}: {
  currentStep: number
  isComplete: boolean
  onStepChange: (step: number) => void
  steps: readonly OnboardingStep[]
}) {
  return (
    <Stepper
      value={currentStep}
      onValueChange={onStepChange}
      orientation="vertical"
      className="flex w-full flex-col items-start justify-center gap-0"
      indicators={{
        completed: (
          <CheckIcon className="size-3" aria-hidden="true" />
        ),
      }}
    >
      <StepperNav aria-label="Onboarding progress" className="w-full">
        {steps.map((step) => {
          const description =
            SIDEBAR_STEP_DESCRIPTIONS[step.id] ?? step.description

          return (
            <StepperItem
              key={step.id}
              step={step.value}
              completed={isComplete || currentStep > step.value}
              className="relative items-start not-last:flex-1"
            >
              <StepperTrigger className="w-full items-start gap-3 pb-5 text-left last:pb-0">
                <StepperIndicator className="mt-0.5 size-4 bg-transparent text-transparent ring-1 ring-white/35 data-[state=active]:bg-transparent data-[state=active]:ring-white/65 data-[state=completed]:bg-white data-[state=completed]:text-slate-950 data-[state=completed]:ring-white/75">
                  {step.value === currentStep && !isComplete ? (
                    <span
                      className="block size-1.5 rounded-full bg-white"
                      aria-hidden="true"
                    />
                  ) : (
                    <span className="sr-only">{step.value}</span>
                  )}
                </StepperIndicator>
                <div className="min-w-0 flex-1 text-left">
                  <StepperTitle className="!text-label !leading-4 text-white data-[state=completed]:text-white/88 data-[state=inactive]:text-white/72">
                    {step.label}
                  </StepperTitle>
                  <StepperDescription className="mt-0.5 max-w-none !text-caption !leading-4 text-white/50 data-[state=active]:text-white/60">
                    {description}
                  </StepperDescription>
                </div>
              </StepperTrigger>
              {step.value < steps.length ? (
                <StepperSeparator className="absolute top-6 bottom-1 left-2 -order-1 m-0 !h-[calc(100%-1.75rem)] w-px -translate-x-1/2 bg-white/20 group-data-[state=completed]/step:bg-white/55" />
              ) : null}
            </StepperItem>
          )
        })}
      </StepperNav>
    </Stepper>
  )
}