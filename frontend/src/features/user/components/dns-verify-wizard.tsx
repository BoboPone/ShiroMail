import { useCallback, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CheckCircle2, Copy, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { showSuccess } from "@/lib/toast";
import { verifyDomain, type DomainOption, type DomainVerificationResult } from "../api";

type DnsProvider = "cloudflare" | "namecheap" | "godaddy" | "route53" | "other";

type RequiredRecord = {
  type: string;
  name: string;
  value: string;
  priority?: number;
};

type DnsVerifyWizardProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainOption;
  requiredRecords?: RequiredRecord[];
};

const WIZARD_STEPS = ["provider", "records", "verify"] as const;

function getDefaultRecords(domainName: string): RequiredRecord[] {
  return [
    { type: "MX", name: domainName, value: `mail.${domainName}`, priority: 10 },
    { type: "TXT", name: domainName, value: `v=spf1 include:${domainName} ~all` },
    { type: "TXT", name: `_dmarc.${domainName}`, value: `v=DMARC1; p=none; rua=mailto:postmaster@${domainName}` },
  ];
}

function getProviderInstructions(provider: DnsProvider, t: (key: string) => string): string {
  const map: Record<DnsProvider, string> = {
    cloudflare: t("dns.verifyWizard.instructionsCloudflare"),
    namecheap: t("dns.verifyWizard.instructionsNamecheap"),
    godaddy: t("dns.verifyWizard.instructionsGodaddy"),
    route53: t("dns.verifyWizard.instructionsRoute53"),
    other: t("dns.verifyWizard.instructionsOther"),
  };
  return map[provider];
}

export function DnsVerifyWizard({ open, onOpenChange, domain, requiredRecords }: DnsVerifyWizardProps) {
  const { t } = useTranslation();
  const [step, setStep] = useState<number>(0);
  const [provider, setProvider] = useState<DnsProvider | "">("");
  const [verifyResult, setVerifyResult] = useState<DomainVerificationResult | null>(null);

  const records = requiredRecords ?? getDefaultRecords(domain.domain);

  const verifyMutation = useMutation({
    mutationFn: () => verifyDomain(domain.id),
    onSuccess: (result) => {
      setVerifyResult(result);
    },
  });

  const handleCopy = useCallback(async (value: string) => {
    if (typeof navigator === "undefined" || !navigator.clipboard) return;
    try {
      await navigator.clipboard.writeText(value);
      showSuccess(t("dns.copied"));
    } catch {
      // silently fail
    }
  }, [t]);

  const handleNext = () => {
    if (step < WIZARD_STEPS.length - 1) {
      setStep(step + 1);
    }
  };

  const handleBack = () => {
    if (step > 0) {
      setStep(step - 1);
      setVerifyResult(null);
    }
  };

  const handleOpenChange = (value: boolean) => {
    if (!value) {
      setStep(0);
      setProvider("");
      setVerifyResult(null);
    }
    onOpenChange(value);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("dns.verifyWizard.title")}</DialogTitle>
          <DialogDescription>{t("dns.verifyWizard.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2 py-2">
          {WIZARD_STEPS.map((s, i) => (
            <div key={s} className="flex items-center gap-1">
              <div
                className={`flex size-6 items-center justify-center rounded-full text-xs font-medium ${
                  i <= step ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
                }`}
              >
                {i + 1}
              </div>
              <span className={`text-xs ${i <= step ? "text-foreground" : "text-muted-foreground"}`}>
                {t(`dns.verifyWizard.step${s.charAt(0).toUpperCase() + s.slice(1)}` as never)}
              </span>
              {i < WIZARD_STEPS.length - 1 && <div className="mx-1 h-px w-4 bg-border" />}
            </div>
          ))}
        </div>

        <div className="min-h-[200px] space-y-4 py-2">
          {step === 0 && (
            <div className="space-y-3">
              <Select value={provider} onValueChange={(v) => setProvider(v as DnsProvider)}>
                <SelectTrigger>
                  <SelectValue placeholder={t("dns.verifyWizard.selectProvider")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cloudflare">{t("dns.verifyWizard.providerCloudflare")}</SelectItem>
                  <SelectItem value="namecheap">{t("dns.verifyWizard.providerNamecheap")}</SelectItem>
                  <SelectItem value="godaddy">{t("dns.verifyWizard.providerGodaddy")}</SelectItem>
                  <SelectItem value="route53">{t("dns.verifyWizard.providerRoute53")}</SelectItem>
                  <SelectItem value="other">{t("dns.verifyWizard.providerOther")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}

          {step === 1 && (
            <div className="space-y-3">
              {provider && (
                <p className="text-sm text-muted-foreground">
                  {getProviderInstructions(provider as DnsProvider, t)}
                </p>
              )}
              <p className="text-sm font-medium">{t("dns.verifyWizard.requiredRecords")}</p>
              <div className="space-y-2">
                {records.map((record, index) => (
                  <div key={index} className="group/row flex items-center gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs">
                    <span className="w-10 shrink-0 font-mono font-medium">{record.type}</span>
                    <span className="w-32 shrink-0 truncate font-mono text-muted-foreground">{record.name}</span>
                    <span className="flex-1 truncate font-mono text-muted-foreground">{record.value}</span>
                    {record.priority !== undefined && (
                      <span className="shrink-0 text-muted-foreground">{record.priority}</span>
                    )}
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      className="opacity-0 group-hover/row:opacity-100 transition-opacity"
                      onClick={() => handleCopy(record.value)}
                      aria-label={t("dns.verifyWizard.copyValue")}
                    >
                      <Copy className="size-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {step === 2 && (
            <div className="space-y-4">
              {!verifyResult && !verifyMutation.isPending && (
                <p className="text-sm text-muted-foreground">
                  {t("dns.verifyWizard.description")}
                </p>
              )}
              {verifyMutation.isPending && (
                <p className="text-sm text-muted-foreground">{t("dns.verifyWizard.verifying")}</p>
              )}
              {verifyResult && (
                <div className="space-y-3">
                  {verifyResult.passed ? (
                    <div className="flex items-center gap-2 text-sm text-green-600">
                      <CheckCircle2 className="size-4" />
                      <span>{t("dns.verifyWizard.verifySuccess")}</span>
                    </div>
                  ) : (
                    <div className="space-y-2">
                      <div className="flex items-center gap-2 text-sm text-destructive">
                        <XCircle className="size-4" />
                        <span>{t("dns.verifyWizard.verifyFailed")}</span>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {t("dns.verifyWizard.verifyPartial", {
                          verified: verifyResult.verifiedCount,
                          total: verifyResult.totalCount,
                        })}
                      </p>
                    </div>
                  )}
                </div>
              )}
              {!verifyMutation.isPending && (
                <Button
                  onClick={() => verifyMutation.mutate()}
                  disabled={verifyMutation.isPending}
                >
                  {t("dns.verifyWizard.verifyButton")}
                </Button>
              )}
            </div>
          )}
        </div>

        <DialogFooter className="flex-row justify-between">
          <div>
            {step > 0 && (
              <Button variant="outline" onClick={handleBack}>
                {t("dns.verifyWizard.back")}
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <DialogClose asChild>
              <Button variant="ghost">{t("dns.verifyWizard.close")}</Button>
            </DialogClose>
            {step < WIZARD_STEPS.length - 1 && (
              <Button onClick={handleNext} disabled={step === 0 && !provider}>
                {t("dns.verifyWizard.next")}
              </Button>
            )}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
