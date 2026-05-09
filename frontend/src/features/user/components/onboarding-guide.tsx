import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { CheckCircle2, Circle, Globe, Mail, Shield, X } from "lucide-react";

type OnboardingStep = {
  key: string;
  icon: typeof Globe;
  done: boolean;
};

type Props = {
  hasDomains: boolean;
  hasMailboxes: boolean;
  hasApiKeys: boolean;
};

export function OnboardingGuide({ hasDomains, hasMailboxes, hasApiKeys }: Props) {
  const { t } = useTranslation();
  const [dismissed, setDismissed] = useState(() => {
    return sessionStorage.getItem("onboarding-dismissed") === "1";
  });

  const steps: OnboardingStep[] = [
    { key: "domain", icon: Globe, done: hasDomains },
    { key: "mailbox", icon: Mail, done: hasMailboxes },
    { key: "apiKey", icon: Shield, done: hasApiKeys },
  ];

  const allDone = steps.every((s) => s.done);
  if (dismissed || allDone) return null;

  const currentStep = steps.findIndex((s) => !s.done);

  return (
    <Card className="border-primary/20 bg-primary/5">
      <CardContent className="relative py-5">
        <Button
          size="icon-sm"
          variant="ghost"
          className="absolute right-3 top-3"
          onClick={() => {
            setDismissed(true);
            sessionStorage.setItem("onboarding-dismissed", "1");
          }}
        >
          <X className="size-4" />
        </Button>

        <div className="mb-4">
          <h3 className="text-sm font-semibold">{t("onboarding.title")}</h3>
          <p className="text-sm text-muted-foreground">{t("onboarding.description")}</p>
        </div>

        <div className="space-y-3">
          {steps.map((step, index) => {
            const Icon = step.icon;
            const StepIcon = step.done ? CheckCircle2 : Circle;
            const isActive = index === currentStep;

            return (
              <div
                key={step.key}
                className={`flex items-center gap-3 rounded-lg px-3 py-2 ${
                  isActive ? "bg-background shadow-sm" : ""
                }`}
              >
                <StepIcon
                  className={`size-5 shrink-0 ${
                    step.done ? "text-primary" : isActive ? "text-primary/70" : "text-muted-foreground/50"
                  }`}
                />
                <Icon className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className={`text-sm ${isActive ? "font-medium" : ""}`}>
                    {t(`onboarding.steps.${step.key}.title`)}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t(`onboarding.steps.${step.key}.hint`)}
                  </p>
                </div>
                {isActive && (
                  <Button asChild size="sm" variant="outline">
                    <Link to={stepLink(step.key)}>{t("onboarding.go")}</Link>
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      </CardContent>
    </Card>
  );
}

function stepLink(key: string) {
  switch (key) {
    case "domain": return "/dashboard/domains";
    case "mailbox": return "/dashboard/mailboxes";
    case "apiKey": return "/dashboard/api-keys";
    default: return "/dashboard";
  }
}
