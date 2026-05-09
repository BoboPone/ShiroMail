import { useCallback, useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { showSuccess } from "@/lib/toast";
import { updateMailboxForwarding, type MailboxItem } from "../api";

type MailboxForwardingSettingsProps = {
  mailbox: MailboxItem;
};

const EMAIL_REGEX = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function MailboxForwardingSettings({ mailbox }: MailboxForwardingSettingsProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(!!mailbox.forwardTo);
  const [forwardTo, setForwardTo] = useState(mailbox.forwardTo ?? "");
  const [keepCopy, setKeepCopy] = useState(mailbox.forwardKeepCopy ?? true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setEnabled(!!mailbox.forwardTo);
    setForwardTo(mailbox.forwardTo ?? "");
    setKeepCopy(mailbox.forwardKeepCopy ?? true);
  }, [mailbox.forwardTo, mailbox.forwardKeepCopy]);

  const mutation = useMutation({
    mutationFn: () =>
      updateMailboxForwarding(mailbox.id, {
        forwardTo: enabled ? forwardTo.trim() : "",
        forwardKeepCopy: keepCopy,
      }),
    onSuccess: () => {
      showSuccess(t("forwarding.saved"));
      queryClient.invalidateQueries({ queryKey: ["user-dashboard"] });
    },
  });

  const handleSave = useCallback(() => {
    setError(null);
    if (enabled && !EMAIL_REGEX.test(forwardTo.trim())) {
      setError(t("forwarding.invalidEmail"));
      return;
    }
    mutation.mutate();
  }, [enabled, forwardTo, mutation, t]);

  return (
    <div className="space-y-4 rounded-xl border border-border/60 bg-background/80 p-4">
      <div className="space-y-1">
        <p className="text-sm font-medium">{t("forwarding.title")}</p>
        <p className="text-xs text-muted-foreground">{t("forwarding.description")}</p>
      </div>

      <div className="flex items-center gap-2">
        <Checkbox
          id="forwarding-enabled"
          checked={enabled}
          onCheckedChange={(checked) => setEnabled(checked === true)}
        />
        <Label htmlFor="forwarding-enabled" className="text-sm cursor-pointer">
          {t("forwarding.enabled")}
        </Label>
      </div>

      {enabled && (
        <div className="space-y-3 pl-6">
          <div className="space-y-1.5">
            <Label htmlFor="forward-to" className="text-xs">
              {t("forwarding.forwardTo")}
            </Label>
            <Input
              id="forward-to"
              type="email"
              placeholder={t("forwarding.forwardToPlaceholder")}
              value={forwardTo}
              onChange={(e) => {
                setForwardTo(e.target.value);
                setError(null);
              }}
            />
            {error && <p className="text-xs text-destructive">{error}</p>}
          </div>

          <div className="flex items-center gap-2">
            <Checkbox
              id="forward-keep-copy"
              checked={keepCopy}
              onCheckedChange={(checked) => setKeepCopy(checked === true)}
            />
            <Label htmlFor="forward-keep-copy" className="text-xs cursor-pointer">
              {t("forwarding.keepCopy")}
            </Label>
          </div>
        </div>
      )}

      <Button
        size="sm"
        onClick={handleSave}
        disabled={mutation.isPending}
      >
        {mutation.isPending ? t("forwarding.saving") : t("forwarding.save")}
      </Button>
    </div>
  );
}
