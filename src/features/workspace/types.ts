import type { components } from '@/shared/api/schema';
export type Reference = components['schemas']['ReferenceData'];
export type WorkOrder = components['schemas']['Order'];
export type Staff = components['schemas']['Staff'];
export type PlanQuality = {
  weighted_visits: number;
  visits: number;
  travel_sec: number;
  utilization_cv: number;
};
// Optional UI extension; the organizer's generated contract stays unchanged.
export type Plan = components['schemas']['Plan'] & {
  optimization?: {
    engine: 'pyvrp_go' | 'go_baseline';
    status: string;
    baseline: PlanQuality;
    selected: PlanQuality;
  };
};
export type Route = components['schemas']['Route'];
export type Stop = components['schemas']['Stop'];
export type Alert = components['schemas']['Alert'];
export type Event = components['schemas']['Event'];
export type EventResult = components['schemas']['EventResult'];
export type Day = components['schemas']['DayInput'];
export type DaySummary = {
  clock_mode?: 'DEMO' | 'LIVE';
  date: string;
  as_of: string;
  plan_version: number;
  event_count: number;
  orders_total?: number;
  staff_total?: number;
  source_date?: string;
};
export type VisitFact = {
  order_id: string;
  staff_id: string;
  status: 'EN_ROUTE' | 'ARRIVED' | 'IN_PROGRESS' | 'COMPLETED';
  departed_at?: string;
  arrived_at?: string;
  started_at?: string | null;
  completed_at?: string;
  report: string;
};
export type OrderContact = {
  order_id: string;
  name: string;
  phone: string;
  description: string;
};
export type Snapshot = {
  ratings?: Record<string, { average: number; count: number }>;
  revision?: number;
  staff_change_count?: number;
  staff_changes?: StaffChangeRecord[];
  assignment_count?: number;
  assignments?: {
    request_id: string;
    order_id: string;
    actor_id: string;
    actor_name: string;
    from_staff?: string;
    to_staff: string;
    reason: string;
    as_of: string;
    plan_version: number;
  }[];
  facts?: VisitFact[];
  contacts?: OrderContact[];
  day: Day;
  reference: Reference;
  plan: Plan | null;
  events: Event[];
  generation: number;
  event_count: number;
};

export type StaffVisitChange = {
  order_id: string;
  before_staff?: string;
  after_staff?: string;
  before_start?: string;
  before_end?: string;
  after_start?: string;
  after_end?: string;
  reason?: string;
};
export type StaffChangeRecord = {
  request_id: string;
  staff_id: string;
  actor_id: string;
  actor_name: string;
  reason: string;
  as_of: string;
  plan_version: number;
  revision: number;
  before: Staff;
  after: Staff;
  absent_before: boolean;
  absent_after: boolean;
  changes: StaffVisitChange[];
};
