export type ScheduleType = "once" | "recurring";

export interface Schedule {
  id: string;
  workspace_id: string;
  workflow_id: string;
  name: string;
  description: string;
  schedule_type: ScheduleType;
  cron_expr: string | null;
  once_at: string | null;
  timezone: string;
  weekdays: number[];
  time_of_day: string;
  next_run_at: string;
  last_run_at: string | null;
  enabled: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface CreateScheduleRequest {
  workflow_id: string;
  name: string;
  description?: string;
  schedule_type: ScheduleType;
  cron_expr?: string;
  once_at?: string;
  timezone?: string;
  weekdays?: number[];
  time_of_day?: string;
}

export interface UpdateScheduleRequest {
  name?: string;
  description?: string;
  schedule_type?: ScheduleType;
  cron_expr?: string;
  once_at?: string;
  timezone?: string;
  weekdays?: number[];
  time_of_day?: string;
}

export interface ListSchedulesResponse {
  schedules: Schedule[];
  total: number;
}
