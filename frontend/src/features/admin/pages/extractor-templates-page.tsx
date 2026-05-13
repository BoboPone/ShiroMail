import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { PaginationControls } from "@/components/ui/pagination-controls";
import { Textarea } from "@/components/ui/textarea";
import { BasicSelect } from "@/components/ui/basic-select";
import {
  WorkspaceBadge,
  WorkspaceEmpty,
  WorkspaceField,
  WorkspacePage,
  WorkspacePanel,
} from "@/components/layout/workspace-ui";
import { getAPIErrorMessage } from "@/lib/http";
import { paginateItems } from "@/lib/pagination";
import { useURLPagination } from "@/hooks/use-url-pagination";
import {
  createAdminMailExtractorRule,
  deleteAdminMailExtractorRule,
  fetchAdminMailExtractorRules,
  fetchAdminMailboxMessages,
  fetchAdminMailboxes,
  testAdminMailExtractorRule,
  updateAdminMailExtractorRule,
  type AdminMailbox,
} from "../api";
import type { MailExtractorRule } from "../../user/api";
import { emptyRuleDraft, normalizeMailExtractorRule, toRuleDraft, validateRuleDraft, type RuleDraft } from "../../user/extractor-rule-form";

const targetFieldOptions = [
  { value: "subject", label: "标题" },
  { value: "from_addr", label: "发件人" },
  { value: "to_addr", label: "收件人" },
  { value: "text_body", label: "正文" },
  { value: "html_text", label: "HTML 文本" },
  { value: "raw_text", label: "Raw" },
] as const;

const ADMIN_EXTRACTOR_TEMPLATES_PAGE_SIZE = 20;
const PAGE_SIZE_OPTIONS = [10, 20, 50];

export function AdminExtractorTemplatesPage() {
  const queryClient = useQueryClient();
  const [selectedRuleId, setSelectedRuleId] = useState<number | "new">("new");
  const { page: rulesPage, pageSize: rulesPageSize, setPage: setRulesPage, setPageSize: setRulesPageSize } = useURLPagination({
    defaultPage: 1,
    defaultPageSize: ADMIN_EXTRACTOR_TEMPLATES_PAGE_SIZE,
  });
  const [draft, setDraft] = useState<RuleDraft>(emptyRuleDraft);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [sampleMailboxId, setSampleMailboxId] = useState("");
  const [sampleMessageId, setSampleMessageId] = useState("");

  const rulesQuery = useQuery({
    queryKey: ["admin-mail-extractor-rules"],
    queryFn: fetchAdminMailExtractorRules,
  });
  const mailboxesQuery = useQuery({
    queryKey: ["admin-mailboxes"],
    queryFn: fetchAdminMailboxes,
  });

  const mailboxes = (mailboxesQuery.data ?? []).filter((mailbox) => mailbox.status === "active");
  const resolvedMailboxId = sampleMailboxId ? Number(sampleMailboxId) : mailboxes[0]?.id;
  const messagesQuery = useQuery({
    queryKey: ["admin-extractor-template-messages", resolvedMailboxId],
    queryFn: () => fetchAdminMailboxMessages(resolvedMailboxId!),
    enabled: Boolean(resolvedMailboxId),
    staleTime: 10_000,
  });

  const saveMutation = useMutation({
    mutationFn: async () => {
      const payload = {
        ...draft,
        captureGroupIndex: draft.resultMode === "capture_group" ? Number(draft.captureGroupIndex ?? 1) : undefined,
      };
      if (selectedRuleId === "new") {
        return createAdminMailExtractorRule(payload);
      }
      return updateAdminMailExtractorRule(selectedRuleId, payload);
    },
    onSuccess: async (savedRule) => {
      setFeedback("默认提取模板已保存。");
      setSelectedRuleId(savedRule.id);
      setDraft(toRuleDraft(savedRule));
      await queryClient.invalidateQueries({ queryKey: ["admin-mail-extractor-rules"] });
    },
    onError: (error) => setFeedback(getAPIErrorMessage(error, "保存默认模板失败。")),
  });

  const deleteMutation = useMutation({
    mutationFn: async (ruleId: number) => deleteAdminMailExtractorRule(ruleId),
    onSuccess: async () => {
      setFeedback("默认模板已删除。");
      setSelectedRuleId("new");
      setDraft(emptyRuleDraft());
      await queryClient.invalidateQueries({ queryKey: ["admin-mail-extractor-rules"] });
    },
    onError: (error) => setFeedback(getAPIErrorMessage(error, "删除默认模板失败。")),
  });

  const testMutation = useMutation({
    mutationFn: async () =>
      testAdminMailExtractorRule(
        {
          ...draft,
          captureGroupIndex: draft.resultMode === "capture_group" ? Number(draft.captureGroupIndex ?? 1) : undefined,
        },
        resolvedMailboxId && sampleMessageId
          ? { mailboxId: Number(resolvedMailboxId), messageId: Number(sampleMessageId) }
          : {},
      ),
    onError: (error) => setFeedback(getAPIErrorMessage(error, "测试默认模板失败。")),
  });

  const rules = (rulesQuery.data ?? []).map(normalizeMailExtractorRule);
  const messages = messagesQuery.data ?? [];
  const paginatedRules = useMemo(
    () => paginateItems(rules, rulesPage, rulesPageSize),
    [rules, rulesPage, rulesPageSize],
  );

  function selectRule(rule: MailExtractorRule) {
    setSelectedRuleId(rule.id);
    setDraft(toRuleDraft(rule));
    setFeedback(null);
  }

  function handleSave() {
    const error = validateRuleDraft(draft);
    if (error) {
      setFeedback(error);
      return;
    }
    saveMutation.mutate();
  }

  function handleTest() {
    const error = validateRuleDraft(draft);
    if (error) {
      setFeedback(error);
      return;
    }
    testMutation.mutate();
  }

  return (
    <WorkspacePage>
      <WorkspacePanel
        title="提取模板"
        description="管理员维护可供用户启用或复制的默认提取规则模板。"
        action={
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setSelectedRuleId("new");
              setDraft(emptyRuleDraft());
              setFeedback(null);
            }}
          >
            <Plus className="size-4" />
            新建模板
          </Button>
        }
      >
        {feedback ? (
          <div className="rounded-xl border border-border/60 bg-muted/10 px-3 py-2 text-sm text-muted-foreground">
            {feedback}
          </div>
        ) : null}

        <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
          <div className="space-y-3">
            {rules.length ? (
              <>
                {paginatedRules.items.map((rule) => (
                <button key={rule.id} type="button" className="block w-full text-left" onClick={() => selectRule(rule)}>
                  <Card className={selectedRuleId === rule.id ? "border-primary/40 bg-muted/20 shadow-none" : "border-border/60 bg-muted/10 shadow-none"}>
                    <CardContent className="space-y-2 py-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="space-y-1">
                          <div className="text-sm font-medium">{rule.name}</div>
                          <div className="text-xs text-muted-foreground">{rule.label || "未设置标签"}</div>
                        </div>
                        <WorkspaceBadge variant={rule.enabled ? "secondary" : "outline"}>
                          {rule.enabled ? "启用" : "停用"}
                        </WorkspaceBadge>
                      </div>
                      <div className="text-xs text-muted-foreground">{rule.targetFields.join(" / ") || "未选择字段"}</div>
                    </CardContent>
                  </Card>
                </button>
                ))}
                <PaginationControls
                  itemLabel="模板"
                  onPageChange={setRulesPage}
                  onPageSizeChange={setRulesPageSize}
                  page={paginatedRules.page}
                  pageSize={rulesPageSize}
                  pageSizeOptions={PAGE_SIZE_OPTIONS}
                  showPageSizeSelector
                  total={paginatedRules.total}
                  totalPages={paginatedRules.totalPages}
                />
              </>
            ) : (
              <WorkspaceEmpty title="暂无默认模板" description="这里创建的模板会显示给所有用户，但只有用户主动启用后才生效。" />
            )}
          </div>

          <div className="space-y-4">
            <Card className="border-border/60 bg-muted/10 shadow-none">
              <CardContent className="space-y-4 py-4">
                <div className="text-sm font-medium">{selectedRuleId === "new" ? "新建默认模板" : "编辑默认模板"}</div>

                <div className="grid gap-3 md:grid-cols-2">
                  <WorkspaceField label="模板名称">
                    <Input aria-label="模板名称" value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
                  </WorkspaceField>
                  <WorkspaceField label="结果标签">
                    <Input aria-label="结果标签" value={draft.label} onChange={(event) => setDraft((current) => ({ ...current, label: event.target.value }))} />
                  </WorkspaceField>
                </div>

                <WorkspaceField label="描述">
                  <Textarea aria-label="模板描述" rows={3} value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
                </WorkspaceField>

                <WorkspaceField label="提取字段">
                  <div className="grid gap-2 md:grid-cols-2">
                    {targetFieldOptions.map((option) => (
                      <label className="flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2 text-sm" key={option.value}>
                        <Checkbox
                          checked={draft.targetFields.includes(option.value)}
                          onCheckedChange={(checked) =>
                            setDraft((current) => ({
                              ...current,
                              targetFields: checked === true
                                ? [...current.targetFields, option.value]
                                : current.targetFields.filter((item) => item !== option.value),
                            }))
                          }
                        />
                        <span>{option.label}</span>
                      </label>
                    ))}
                  </div>
                </WorkspaceField>

                <div className="grid gap-3 md:grid-cols-3">
                  <WorkspaceField label="Flags">
                    <Input aria-label="正则 Flags" value={draft.flags} onChange={(event) => setDraft((current) => ({ ...current, flags: event.target.value }))} />
                  </WorkspaceField>
                  <WorkspaceField label="结果模式">
                    <BasicSelect value={draft.resultMode} onChange={(event) => setDraft((current) => ({ ...current, resultMode: event.target.value }))}>
                      <option value="first_match">首个匹配</option>
                      <option value="all_matches">全部匹配</option>
                      <option value="capture_group">指定分组</option>
                    </BasicSelect>
                  </WorkspaceField>
                  <WorkspaceField label="捕获分组">
                    <Input
                      type="number"
                      aria-label="捕获分组"
                      value={String(draft.captureGroupIndex ?? 1)}
                      onChange={(event) => setDraft((current) => ({ ...current, captureGroupIndex: Number(event.target.value || 0) }))}
                      disabled={draft.resultMode !== "capture_group"}
                    />
                  </WorkspaceField>
                </div>

                <WorkspaceField label="正则表达式">
                  <Textarea aria-label="正则表达式" rows={5} value={draft.pattern} onChange={(event) => setDraft((current) => ({ ...current, pattern: event.target.value }))} placeholder="例如：\\b(\\d{6})\\b" />
                </WorkspaceField>

                <div className="grid gap-3 md:grid-cols-2">
                  <WorkspaceField label="发件人包含">
                    <Input aria-label="发件人包含" value={draft.senderContains} onChange={(event) => setDraft((current) => ({ ...current, senderContains: event.target.value }))} placeholder="仅支持普通文本包含，如 noreply@x.ai" />
                  </WorkspaceField>
                  <WorkspaceField label="标题包含">
                    <Input aria-label="标题包含" value={draft.subjectContains} onChange={(event) => setDraft((current) => ({ ...current, subjectContains: event.target.value }))} placeholder="仅支持普通文本包含，如 verification code" />
                  </WorkspaceField>
                </div>

                <div className="grid gap-3 md:grid-cols-2">
                  <WorkspaceField label="作用域邮箱（可选）">
                    <BasicSelect
                      value={draft.mailboxIds[0] ? String(draft.mailboxIds[0]) : ""}
                      onChange={(event) =>
                        setDraft((current) => ({
                          ...current,
                          mailboxIds: event.target.value ? [Number(event.target.value)] : [],
                        }))
                      }
                    >
                      <option value="">全部邮箱</option>
                      {mailboxes.map((mailbox: AdminMailbox) => (
                        <option key={mailbox.id} value={mailbox.id}>
                          {mailbox.address}
                        </option>
                      ))}
                    </BasicSelect>
                  </WorkspaceField>
                  <WorkspaceField label="排序权重">
                    <Input aria-label="排序权重" type="number" value={String(draft.sortOrder)} onChange={(event) => setDraft((current) => ({ ...current, sortOrder: Number(event.target.value || 0) }))} />
                  </WorkspaceField>
                </div>

                <label className="flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2 text-sm">
                  <Checkbox checked={draft.enabled} onCheckedChange={(checked) => setDraft((current) => ({ ...current, enabled: checked === true }))} />
                  <span>启用此默认模板</span>
                </label>

                <div className="flex flex-wrap gap-2">
                  <Button onClick={handleSave} disabled={saveMutation.isPending}>
                    {saveMutation.isPending ? "保存中…" : "保存模板"}
                  </Button>
                  {selectedRuleId !== "new" ? (
                    <Button variant="outline" onClick={() => deleteMutation.mutate(Number(selectedRuleId))} disabled={deleteMutation.isPending}>
                      删除模板
                    </Button>
                  ) : null}
                </div>
              </CardContent>
            </Card>

            <Card className="border-border/60 bg-muted/10 shadow-none">
              <CardContent className="space-y-4 py-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium">模板测试</div>
                  <Button size="sm" variant="outline" onClick={() => void rulesQuery.refetch()}>
                    <RefreshCw className={`size-4 ${rulesQuery.isFetching ? "animate-spin" : ""}`} />
                    刷新
                  </Button>
                </div>

                <div className="grid gap-3 md:grid-cols-2">
                  <WorkspaceField label="测试邮箱">
                    <BasicSelect value={sampleMailboxId || (resolvedMailboxId ? String(resolvedMailboxId) : "")} onChange={(event) => {
                      setSampleMailboxId(event.target.value);
                      setSampleMessageId("");
                    }}>
                      {mailboxes.map((mailbox) => (
                        <option key={mailbox.id} value={mailbox.id}>
                          {mailbox.address}
                        </option>
                      ))}
                    </BasicSelect>
                  </WorkspaceField>
                  <WorkspaceField label="测试邮件">
                    <BasicSelect value={sampleMessageId} onChange={(event) => setSampleMessageId(event.target.value)}>
                      <option value="">选择一封邮件</option>
                      {messages.map((message) => (
                        <option key={message.id} value={message.id}>
                          {(message.subject || "(无主题)").slice(0, 40)}
                        </option>
                      ))}
                    </BasicSelect>
                  </WorkspaceField>
                </div>

                <Button onClick={handleTest} disabled={testMutation.isPending || !sampleMessageId}>
                  {testMutation.isPending ? "测试中…" : "运行模板测试"}
                </Button>

                {testMutation.data?.items.length ? (
                  <div className="space-y-2">
                    {testMutation.data.items.map((item, index) => (
                      <div className="rounded-xl border border-border/60 bg-background/60 px-3 py-3" key={`${item.ruleId}-${index}`}>
                        <div className="flex flex-wrap items-center gap-2">
                          <WorkspaceBadge variant="outline">{item.label || item.ruleName}</WorkspaceBadge>
                          <span className="text-xs text-muted-foreground">{item.sourceField}</span>
                        </div>
                        <div className="mt-2 whitespace-pre-wrap break-all text-sm leading-6">{item.values?.join("\n") || item.value}</div>
                      </div>
                    ))}
                  </div>
                ) : testMutation.isSuccess ? (
                  <WorkspaceEmpty title="没有命中结果" description="当前测试邮件没有匹配到此默认模板。" />
                ) : (
                  <WorkspaceEmpty title="选择一封邮件开始测试" description="管理员可以先对样例邮件验证模板效果，再提供给用户启用。" />
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </WorkspacePanel>
    </WorkspacePage>
  );
}
