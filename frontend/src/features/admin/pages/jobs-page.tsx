import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, LoaderCircle, RefreshCw, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { BasicSelect } from "@/components/ui/basic-select";
import { PaginationControls } from "@/components/ui/pagination-controls";
import {
  WorkspaceBadge,
  WorkspaceEmpty,
  WorkspaceField,
  WorkspaceListRow,
  WorkspaceMetric,
  WorkspacePage,
  WorkspacePanel,
} from "@/components/layout/workspace-ui";
import {
  fetchAdminSMTPMetrics,
  fetchAdminInboundSpool,
  fetchAdminJobs,
  retryAdminInboundSpoolItem,
} from "../api";
import {
  describeInboundSpoolFailure,
  describeSMTPRejectedReason,
} from "../smtp-diagnostics";
import { formatDateTime } from "../../user/pages/shared";

const ADMIN_INBOUND_SPOOL_PAGE_SIZE = 10;
const spoolStatusOptions = [
  { label: "全部状态", value: "all" },
  { label: "Pending", value: "pending" },
  { label: "Processing", value: "processing" },
  { label: "Completed", value: "completed" },
  { label: "Failed", value: "failed" },
];
const spoolFailureModeOptions = [
  { label: "全部失败", value: "all" },
  { label: "Retryable", value: "retryable" },
  { label: "Check config", value: "non_retryable" },
];

function buildSpoolStatusVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  switch (status) {
    case "failed":
      return "destructive";
    case "processing":
      return "default";
    case "completed":
      return "secondary";
    default:
      return "outline";
  }
}

export function AdminJobsPage() {
  const queryClient = useQueryClient();
  const [spoolStatus, setSpoolStatus] = useState("all");
  const [spoolFailureMode, setSpoolFailureMode] = useState("all");
  const [spoolPage, setSpoolPage] = useState(1);

  const jobsQuery = useQuery({ queryKey: ["admin-jobs"], queryFn: fetchAdminJobs });
  const smtpMetricsQuery = useQuery({ queryKey: ["admin-smtp-metrics"], queryFn: fetchAdminSMTPMetrics });
  const spoolQuery = useQuery({
    queryKey: ["admin-inbound-spool", spoolStatus, spoolFailureMode, spoolPage],
    queryFn: () =>
      fetchAdminInboundSpool({
        status: spoolStatus,
        failureMode: spoolFailureMode,
        page: spoolPage,
        pageSize: ADMIN_INBOUND_SPOOL_PAGE_SIZE,
      }),
  });

  const retryMutation = useMutation({
    mutationFn: retryAdminInboundSpoolItem,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-inbound-spool"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-jobs"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-smtp-metrics"] }),
      ]);
    },
  });

  const jobs = useMemo(() => jobsQuery.data ?? [], [jobsQuery.data]);
  const smtpMetrics = smtpMetricsQuery.data;
  const spool = spoolQuery.data;
  const totalSpoolPages = Math.max(1, Math.ceil((spool?.total ?? 0) / (spool?.pageSize ?? ADMIN_INBOUND_SPOOL_PAGE_SIZE)));
  const isRefreshing = jobsQuery.isRefetching || spoolQuery.isRefetching || smtpMetricsQuery.isRefetching;
  const failedJobCount = useMemo(() => jobs.filter((item) => item.status === "failed").length, [jobs]);

  async function handleRefresh() {
    await Promise.all([jobsQuery.refetch(), spoolQuery.refetch(), smtpMetricsQuery.refetch()]);
  }

  return (
    <WorkspacePage>
      <div className="grid gap-3 md:grid-cols-4">
        <WorkspaceMetric hint="最近后台任务失败记录数" label="失败任务" value={failedJobCount} />
        <WorkspaceMetric hint="等待 worker 处理的 SMTP 入站数" label="Pending Spool" value={spool?.summary.pending ?? 0} />
        <WorkspaceMetric hint="正在消费中的 SMTP 入站数" label="Processing Spool" value={spool?.summary.processing ?? 0} />
        <WorkspaceMetric hint="最终失败且需要人工介入的入站数" label="Failed Spool" value={spool?.summary.failed ?? 0} />
      </div>

      <WorkspacePanel
        description="统一查看 worker 失败、SMTP 入站积压、失败原因聚合，并支持人工重试失败的 spool 项。"
        title="任务队列"
        action={
          <Button size="sm" type="button" variant="outline" onClick={() => void handleRefresh()}>
            <RefreshCw className={`size-4 ${isRefreshing ? "animate-spin" : ""}`} />
            刷新
          </Button>
        }
      >
        <div className="grid gap-3 lg:grid-cols-[1.2fr_0.8fr]">
          <div className="space-y-3">
            <WorkspacePanel
              className="border-dashed"
              description="从内存 metrics 快照读取 SMTP 会话、拒收与 spool 消费计数，方便快速判断当前收件链是否稳定。"
              title="SMTP 实时指标"
            >
              <div className="grid gap-3 md:grid-cols-3">
                <WorkspaceMetric hint="SMTP listener 已建立的会话数" label="Sessions" value={smtpMetrics?.sessionsStarted ?? 0} />
                <WorkspaceMetric hint="SMTP RCPT TO 成功接受次数" label="Recipients Accepted" value={smtpMetrics?.recipientsAccepted ?? 0} />
                <WorkspaceMetric hint="DATA 阶段累计接收字节数" label="Bytes Received" value={smtpMetrics?.bytesReceived ?? 0} />
              </div>
              <div className="grid gap-3 md:grid-cols-3">
                <WorkspacePanel className="border-dashed" description="direct / spool 两条成功路径的累计计数。" title="Accepted">
                  {smtpMetrics && Object.keys(smtpMetrics.accepted).length ? (
                    <div className="space-y-3">
                      {Object.entries(smtpMetrics.accepted).map(([key, value]) => (
                        <WorkspaceListRow
                          key={key}
                          title={key}
                          meta={<WorkspaceBadge variant="secondary">{value}</WorkspaceBadge>}
                        />
                      ))}
                    </div>
                  ) : (
                    <WorkspaceEmpty description="当前还没有成功 SMTP 投递计数。" title="暂无成功计数" />
                  )}
                </WorkspacePanel>
                <WorkspacePanel className="border-dashed" description="按拒收原因聚合 SMTP 侧 reject。" title="Rejected">
                  {smtpMetrics && ((smtpMetrics.rejectedDetails?.length ?? 0) > 0 || Object.keys(smtpMetrics.rejected).length) ? (
                    <div className="space-y-3">
                      {(smtpMetrics.rejectedDetails?.length
                        ? smtpMetrics.rejectedDetails.map((detail) => ({
                            key: detail.key,
                            value: detail.count,
                            reason: detail.diagnostic,
                          }))
                        : Object.entries(smtpMetrics.rejected).map(([key, value]) => ({
                            key,
                            value,
                            reason: describeSMTPRejectedReason(key),
                          }))).map(({ key, value, reason }) => {
                        return (
                        <WorkspaceListRow
                          key={key}
                          title={reason.title}
                          description={reason.description}
                          meta={<WorkspaceBadge variant="destructive">{value}</WorkspaceBadge>}
                          titleClassName="whitespace-normal"
                          descriptionClassName="whitespace-normal"
                        />
                        );
                      })}
                    </div>
                  ) : (
                    <WorkspaceEmpty description="当前没有 SMTP reject 计数。" title="暂无拒收计数" />
                  )}
                </WorkspacePanel>
                <WorkspacePanel className="border-dashed" description="worker 对 inbound spool 的 completed / failed 结果累计。" title="Spool Worker">
                  {smtpMetrics && Object.keys(smtpMetrics.spoolProcessed).length ? (
                    <div className="space-y-3">
                      {Object.entries(smtpMetrics.spoolProcessed).map(([key, value]) => (
                        <WorkspaceListRow
                          key={key}
                          title={key}
                          meta={<WorkspaceBadge variant={key === "failed" ? "destructive" : "outline"}>{value}</WorkspaceBadge>}
                        />
                      ))}
                    </div>
                  ) : (
                    <WorkspaceEmpty description="当前没有 worker spool 消费计数。" title="暂无 spool 计数" />
                  )}
                </WorkspacePanel>
              </div>
            </WorkspacePanel>

            <WorkspacePanel
              className="border-dashed"
              description="最近 worker / cleanup / spool 的失败与成功记录。"
              title="后台任务历史"
            >
              {jobs.length ? (
                <div className="space-y-3">
                  {jobs.map((item) => {
                    const failure = item.diagnostic ?? (item.errorMessage ? describeInboundSpoolFailure(item.errorMessage) : null);
                    return (
                      <WorkspaceListRow
                        key={item.id}
                        title={`#${item.id} · ${item.jobType}`}
                        description={
                          failure ? (
                            <div className="space-y-1">
                              <p>{failure.title}</p>
                              <p className="text-muted-foreground">{failure.description}</p>
                            </div>
                          ) : (item.errorMessage || "无异常")
                        }
                        meta={
                          <>
                            <WorkspaceBadge variant={item.status === "failed" ? "destructive" : "outline"}>
                              {item.status}
                            </WorkspaceBadge>
                            {failure ? (
                              <WorkspaceBadge variant={failure.retryable ? "outline" : "secondary"}>
                                {failure.retryable ? "Retryable" : "Check config"}
                              </WorkspaceBadge>
                            ) : null}
                            <span>{formatDateTime(item.createdAt)}</span>
                          </>
                        }
                        titleClassName="whitespace-normal"
                        descriptionClassName="whitespace-normal"
                      />
                    );
                  })}
                </div>
              ) : (
                <WorkspaceEmpty description="后台任务当前没有待观察记录。" title="暂无任务记录" />
              )}
            </WorkspacePanel>

            <WorkspacePanel
              className="border-dashed"
              description="SMTP 接收后先入 spool，再由 worker 异步消费；这里可以查看当前积压、失败与重试。"
              title="Inbound Spool"
              action={
                <div className="flex items-center gap-2">
                  <WorkspaceField label="状态过滤">
                    <BasicSelect
                      aria-label="Inbound Spool 状态过滤"
                      className="min-w-36"
                      value={spoolStatus}
                      onChange={(event) => {
                        setSpoolStatus(event.target.value);
                        setSpoolPage(1);
                      }}
                    >
                      {spoolStatusOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </BasicSelect>
                  </WorkspaceField>
                  <WorkspaceField label="失败诊断">
                    <BasicSelect
                      aria-label="Inbound Spool 失败诊断过滤"
                      className="min-w-36"
                      value={spoolFailureMode}
                      onChange={(event) => {
                        setSpoolFailureMode(event.target.value);
                        setSpoolPage(1);
                      }}
                    >
                      {spoolFailureModeOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </BasicSelect>
                  </WorkspaceField>
                </div>
              }
            >
              {spool?.items.length ? (
                <div className="space-y-3">
                  {spool.items.map((item) => {
                    const isRetrying = retryMutation.isPending && retryMutation.variables === item.id;
                    const failure = item.diagnostic ?? (item.errorMessage
                      ? describeInboundSpoolFailure(item.errorMessage)
                      : null);
                    return (
                      <WorkspaceListRow
                        key={item.id}
                        title={`#${item.id} · ${item.mailFrom || "unknown sender"}`}
                        description={
                          <div className="space-y-1">
                            <p>收件人：{item.recipients.join(", ") || "无"}</p>
                            <p>
                              尝试：{item.attemptCount} / {item.maxAttempts}
                              {failure ? ` · 问题：${failure.title}` : ""}
                            </p>
                            {failure ? (
                              <div className="flex flex-wrap items-center gap-2 text-muted-foreground">
                                <span>{failure.description}</span>
                                <WorkspaceBadge variant={failure.retryable ? "outline" : "secondary"}>
                                  {failure.retryable ? "Retryable" : "Check config"}
                                </WorkspaceBadge>
                              </div>
                            ) : null}
                          </div>
                        }
                        meta={
                          <>
                            <WorkspaceBadge variant={buildSpoolStatusVariant(item.status)}>{item.status}</WorkspaceBadge>
                            <span>{formatDateTime(item.updatedAt)}</span>
                            {item.status === "failed" ? (
                              <Button
                                disabled={isRetrying}
                                size="sm"
                                type="button"
                                variant="outline"
                                onClick={() => retryMutation.mutate(item.id)}
                              >
                                {isRetrying ? (
                                  <LoaderCircle className="size-4 animate-spin" />
                                ) : (
                                  <RotateCcw className="size-4" />
                                )}
                                重试
                              </Button>
                            ) : null}
                          </>
                        }
                      />
                    );
                  })}
                  <PaginationControls
                    itemLabel="spool 项"
                    page={spool.page}
                    pageSize={spool.pageSize}
                    total={spool.total}
                    totalPages={totalSpoolPages}
                    onPageChange={setSpoolPage}
                  />
                </div>
              ) : (
                <WorkspaceEmpty
                  description="当前没有符合筛选条件的 inbound spool 项，说明 SMTP 入站暂时没有积压。"
                  title="Inbound Spool 为空"
                />
              )}
            </WorkspacePanel>
          </div>

          <WorkspacePanel
            className="border-dashed"
            description="聚合最近失败的 spool 错误，优先帮助定位积压主因。"
            title="失败原因聚合"
          >
            {spool?.failureReasons.length ? (
              <div className="space-y-3">
                {spool.failureReasons.map((reason) => {
                  const item = reason.diagnostic ?? describeInboundSpoolFailure(reason.message);
                  return (
                  <WorkspaceListRow
                    key={reason.message}
                    title={item.title}
                    description={item.description}
                    meta={
                      <>
                        <WorkspaceBadge variant="destructive">{reason.count} 次</WorkspaceBadge>
                        <WorkspaceBadge variant={item.retryable ? "outline" : "secondary"}>
                          {item.retryable ? "Retryable" : "Check config"}
                        </WorkspaceBadge>
                        <AlertTriangle className="size-4" />
                      </>
                    }
                    titleClassName="whitespace-normal"
                    descriptionClassName="whitespace-normal"
                  />
                  );
                })}
              </div>
            ) : (
              <WorkspaceEmpty description="当前没有已聚合的 failed spool reason。" title="暂无失败原因" />
            )}
          </WorkspacePanel>
        </div>
      </WorkspacePanel>
    </WorkspacePage>
  );
}
