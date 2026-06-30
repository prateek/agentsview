import type {
  ActivityReport,
  ActivityBucket,
  ActivityReportInterval,
  ActivitySessionRow,
  ActivityKeyMinutes,
} from "../generated/index";

export type Bucket = ActivityBucket;
export type ReportInterval = ActivityReportInterval;
export type SessionRow = Omit<ActivitySessionRow, "models"> & {
  models: string[] | null;
};
export type KeyMinutes = ActivityKeyMinutes;

export type Report = Omit<
  ActivityReport,
  | "buckets"
  | "by_agent"
  | "by_branch"
  | "by_model"
  | "by_project"
  | "by_session"
  | "intervals"
> & {
  buckets: Bucket[] | null;
  by_agent: KeyMinutes[] | null;
  by_branch: KeyMinutes[] | null;
  by_model: KeyMinutes[] | null;
  by_project: KeyMinutes[] | null;
  by_session: SessionRow[] | null;
  intervals: ReportInterval[] | null;
};

function generatedRows<T>(rows: unknown): T[] | null {
  if (!Array.isArray(rows)) return null;
  return rows as T[];
}

function sessionRowsFromGenerated(rows: unknown): SessionRow[] | null {
  const sessions = generatedRows<ActivitySessionRow>(rows);
  if (sessions === null) return null;
  return sessions.map((session) => ({
    ...session,
    models: generatedRows<string>(session.models),
  }));
}

export function activityReportFromGenerated(report: ActivityReport): Report {
  return {
    ...report,
    buckets: generatedRows<Bucket>(report.buckets),
    by_agent: generatedRows<KeyMinutes>(report.by_agent),
    by_branch: generatedRows<KeyMinutes>(report.by_branch),
    by_model: generatedRows<KeyMinutes>(report.by_model),
    by_project: generatedRows<KeyMinutes>(report.by_project),
    by_session: sessionRowsFromGenerated(report.by_session),
    intervals: generatedRows<ReportInterval>(report.intervals),
  };
}
