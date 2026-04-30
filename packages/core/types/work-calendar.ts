export type CalendarDayType = "holiday" | "reduced" | "normal" | "weekend";

export type CalendarSource = "manual" | "pdf_import";

export interface CalendarDay {
  date: string;
  type: CalendarDayType;
  hours: number;
  label?: string;
}

export interface MonthlyHours {
  month: number;
  total_hours: number;
}

export interface WorkCalendar {
  id: string;
  workspace_id: string;
  name: string;
  year: number;
  days: CalendarDay[];
  monthly_hours: MonthlyHours[];
  source: CalendarSource;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkCalendarRequest {
  name: string;
  year: number;
  days: CalendarDay[];
  monthly_hours: MonthlyHours[];
}

export interface UpdateWorkCalendarRequest {
  name: string;
  year: number;
  days: CalendarDay[];
  monthly_hours: MonthlyHours[];
}

export interface ListWorkCalendarsResponse {
  calendars: WorkCalendar[];
}
