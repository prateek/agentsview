import type {
  ActivityReport,
  ActivityBucket,
  ActivityReportInterval,
  ActivitySessionRow,
  ActivityKeyMinutes,
  ActivityBranchKeyMinutes,
} from "../generated/index";

export type Bucket = ActivityBucket;
export type ReportInterval = ActivityReportInterval;
export type SessionRow = Omit<ActivitySessionRow, "models"> & {
  models: string[] | null;
};
export type KeyMinutes = ActivityKeyMinutes;
export type BranchKeyMinutes = ActivityBranchKeyMinutes;

// Report narrows the generated model's `any[] | null` collections (the
// codegen degrades huma's nullable arrays) to their element types. The
// generated ActivityReport stays structurally assignable to Report, so API
// responses need no runtime conversion.
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
  by_branch: BranchKeyMinutes[] | null;
  by_model: KeyMinutes[] | null;
  by_project: KeyMinutes[] | null;
  by_session: SessionRow[] | null;
  intervals: ReportInterval[] | null;
};
