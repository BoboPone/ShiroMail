import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { cn } from "@/lib/utils";
import { Tags, Pencil, Trash2, Plus, Check } from "lucide-react";
import {
  fetchMailboxTags,
  createMailboxTag,
  updateMailboxTag,
  deleteMailboxTag,
  bindMailboxTag,
  unbindMailboxTag,
  type MailboxTag,
} from "../api";

const PRESET_COLORS = [
  "#ef4444",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#06b6d4",
  "#3b82f6",
  "#8b5cf6",
  "#ec4899",
];

// --- MailboxTagBadge ---

export function MailboxTagBadge({
  tag,
  className,
}: {
  tag: Pick<MailboxTag, "name" | "color">;
  className?: string;
}) {
  return (
    <Badge
      className={cn("border-none text-white", className)}
      style={{ backgroundColor: tag.color }}
    >
      {tag.name}
    </Badge>
  );
}

// --- Color Picker ---

function ColorPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (color: string) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {PRESET_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          className={cn(
            "size-6 rounded-full border-2 transition-transform hover:scale-110",
            value === color ? "border-foreground scale-110" : "border-transparent"
          )}
          style={{ backgroundColor: color }}
          onClick={() => onChange(color)}
          aria-label={color}
        />
      ))}
    </div>
  );
}

// --- Tag Form (inline create/edit) ---

function TagForm({
  initial,
  onSubmit,
  onCancel,
  submitLabel,
  isPending,
}: {
  initial?: { name: string; color: string };
  onSubmit: (data: { name: string; color: string }) => void;
  onCancel: () => void;
  submitLabel: string;
  isPending: boolean;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(initial?.name ?? "");
  const [color, setColor] = useState(initial?.color ?? PRESET_COLORS[5]);

  return (
    <div className="space-y-3">
      <Input
        placeholder={t("tags.name")}
        value={name}
        onChange={(e) => setName(e.target.value)}
        autoFocus
      />
      <ColorPicker value={color} onChange={setColor} />
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={isPending}>
          {t("confirm.defaultCancel")}
        </Button>
        <Button
          size="sm"
          disabled={!name.trim() || isPending}
          onClick={() => onSubmit({ name: name.trim(), color })}
        >
          {submitLabel}
        </Button>
      </div>
    </div>
  );
}

// --- MailboxTagManager ---

export function MailboxTagManager() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"list" | "create" | "edit">("list");
  const [editingTag, setEditingTag] = useState<MailboxTag | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MailboxTag | null>(null);

  const { data } = useQuery({
    queryKey: ["mailbox-tags"],
    queryFn: fetchMailboxTags,
    enabled: open,
  });

  const tags = data?.tags ?? [];

  const createMutation = useMutation({
    mutationFn: (input: { name: string; color: string }) => createMailboxTag(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["mailbox-tags"] });
      setMode("list");
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, input }: { id: number; input: { name: string; color: string } }) =>
      updateMailboxTag(id, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["mailbox-tags"] });
      setMode("list");
      setEditingTag(null);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteMailboxTag(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["mailbox-tags"] });
      setDeleteTarget(null);
    },
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      setMode("list");
      setEditingTag(null);
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogTrigger asChild>
          <Button variant="outline" size="sm">
            <Tags className="size-4" />
            {t("tags.title")}
          </Button>
        </DialogTrigger>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("tags.title")}</DialogTitle>
            <DialogDescription className="sr-only">
              {t("tags.title")}
            </DialogDescription>
          </DialogHeader>

          {mode === "list" && (
            <div className="space-y-3">
              {tags.length === 0 ? (
                <p className="text-sm text-muted-foreground py-4 text-center">
                  {t("tags.noTags")}
                </p>
              ) : (
                <ul className="space-y-2">
                  {tags.map((tag) => (
                    <li
                      key={tag.id}
                      className="flex items-center justify-between rounded-lg border px-3 py-2"
                    >
                      <MailboxTagBadge tag={tag} />
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => {
                            setEditingTag(tag);
                            setMode("edit");
                          }}
                          aria-label={t("tags.edit")}
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => setDeleteTarget(tag)}
                          aria-label={t("tags.delete")}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              <DialogFooter>
                <Button size="sm" onClick={() => setMode("create")}>
                  <Plus className="size-4" />
                  {t("tags.create")}
                </Button>
              </DialogFooter>
            </div>
          )}

          {mode === "create" && (
            <TagForm
              submitLabel={t("tags.create")}
              isPending={createMutation.isPending}
              onCancel={() => setMode("list")}
              onSubmit={(input) => createMutation.mutate(input)}
            />
          )}

          {mode === "edit" && editingTag && (
            <TagForm
              initial={{ name: editingTag.name, color: editingTag.color }}
              submitLabel={t("common.save")}
              isPending={updateMutation.isPending}
              onCancel={() => {
                setMode("list");
                setEditingTag(null);
              }}
              onSubmit={(input) =>
                updateMutation.mutate({ id: editingTag.id, input })
              }
            />
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(v) => { if (!v) setDeleteTarget(null); }}
        title={t("tags.delete")}
        description={t("tags.confirmDelete")}
        variant="danger"
        confirmLabel={t("tags.delete")}
        cancelLabel={t("confirm.defaultCancel")}
        loading={deleteMutation.isPending}
        onConfirm={() => {
          if (deleteTarget) deleteMutation.mutate(deleteTarget.id);
        }}
      />
    </>
  );
}

// --- MailboxTagSelector ---

export function MailboxTagSelector({ mailboxId }: { mailboxId: number }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  const { data } = useQuery({
    queryKey: ["mailbox-tags"],
    queryFn: fetchMailboxTags,
    enabled: open,
  });

  const tags = data?.tags ?? [];
  const bindings = data?.bindings ?? [];
  const boundTagIds = new Set(
    bindings.filter((b) => b.mailboxId === mailboxId).map((b) => b.tagId)
  );

  const bindMutation = useMutation({
    mutationFn: (tagId: number) => bindMailboxTag(mailboxId, tagId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mailbox-tags"] }),
  });

  const unbindMutation = useMutation({
    mutationFn: (tagId: number) => unbindMailboxTag(mailboxId, tagId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mailbox-tags"] }),
  });

  function toggleTag(tagId: number) {
    if (boundTagIds.has(tagId)) {
      unbindMutation.mutate(tagId);
    } else {
      bindMutation.mutate(tagId);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon-xs" aria-label={t("tags.assignTag")}>
          <Tags className="size-3.5" />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xs">
        <DialogHeader>
          <DialogTitle>{t("tags.assignTag")}</DialogTitle>
          <DialogDescription className="sr-only">
            {t("tags.assignTag")}
          </DialogDescription>
        </DialogHeader>
        {tags.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center">
            {t("tags.noTags")}
          </p>
        ) : (
          <ul className="space-y-1.5">
            {tags.map((tag) => {
              const bound = boundTagIds.has(tag.id);
              return (
                <li key={tag.id}>
                  <button
                    type="button"
                    className={cn(
                      "flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors hover:bg-muted",
                      bound && "bg-muted"
                    )}
                    onClick={() => toggleTag(tag.id)}
                  >
                    <span
                      className="size-3 rounded-full shrink-0"
                      style={{ backgroundColor: tag.color }}
                    />
                    <span className="flex-1 text-left">{tag.name}</span>
                    {bound && <Check className="size-4 text-primary" />}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
