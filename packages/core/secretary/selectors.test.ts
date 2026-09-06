import { describe, it, expect } from "vitest";
import { SecretaryResponseSchema, type SecretaryResponse } from "./contract";
import { parseWithFallback } from "../api/schema";
import { arrangeSecretaryItems, secretaryDay } from "./selectors";
function fixture(): SecretaryResponse {
  return SecretaryResponseSchema.parse({ revision: 1, unmapped_count: 0, instructions: [], projection: {
    version: 1, state_version: 85, source_as_of: '2026-09-06T08:00:00+08:00',
    cases: [{ id: 'recruitment', title: '客户端招聘', area: '团队', summary: '正式岗位审批' }],
    items: [{ key: 'a', case_id: 'recruitment', kind: 'action', stage: 'ready', owner: 'chairman',
      title: '确认招聘范围', situation: '岗位画像已准备', next_step: '确定范围', source_updated_at: '2026-09-06', due_date: '2026-09-07' }],
  } });
}
function instruct(data: SecretaryResponse, kind: 'complete'|'plan'|'capacity'|'cancel', payload: object, item_key='a') {
  data.instructions.push({sequence: data.instructions.length+1, request_id: String(data.instructions.length), item_key, kind,
    payload: {note: '', ...payload}, created_at:'2026-09-06T08:00:00Z', canonical_receipt: null});
}
describe('secretary workday semantics', () => {
  it('displays a planned deadline once instead of duplicating the todo', () => {
    const d=fixture(); instruct(d,'plan',{scheduled_on:'2026-09-06',estimate_minutes:10});
    const day=secretaryDay(arrangeSecretaryItems(d),d,'2026-09-06');
    expect(day.today).toHaveLength(1);
    const shown=[...day.deadlines,...day.todayDetails,...day.overdueDetails,...day.unplannedDetails];
    expect(shown).toHaveLength(1);
  });
  it('does not put waiting or preparation into personal tasks', () => {
    const d=fixture(); d.projection!.items[0]!.stage='waiting';
    instruct(d,'plan',{scheduled_on:'2026-09-06', estimate_minutes:30});
    expect(secretaryDay(arrangeSecretaryItems(d),d,'2026-09-06').today).toHaveLength(0);
  });
  it('keeps deadlines separate from plans and checks capacity', () => {
    const d=fixture(); instruct(d,'plan',{scheduled_on:'2026-09-06',estimate_minutes:90});
    instruct(d,'capacity',{scheduled_on:'2026-09-06',capacity_minutes:60},'day');
    const items=arrangeSecretaryItems(d); const day=secretaryDay(items,d,'2026-09-06');
    expect(items[0]!.due_date).toBe('2026-09-07'); expect(day.overCapacity).toBe(true);
  });
  it('does not resurrect completed reports on refresh or claim business completion', () => {
    const d=fixture(); instruct(d,'complete',{note:'已提交，等待接收'});
    const item=arrangeSecretaryItems(d)[0]!;
    expect(item.stage).toBe('preparing'); expect(item.reported).toBe('completed');
    expect(item.business_status).toBeUndefined();
    d.revision=2; expect(arrangeSecretaryItems(d)[0]!.stage).toBe('preparing');
  });
  it('keeps cancellations out of deadline reminders', () => {
    const d=fixture(); instruct(d,'cancel',{note:'当前不再需要'});
    expect(secretaryDay(arrangeSecretaryItems(d),d,'2026-09-08').deadlines).toHaveLength(0);
  });
  it('records routine completion in todays results and leaves no verification debt', () => {
    const d=fixture(); d.projection!.items[0]!.closure_mode='self_report';
    instruct(d,'complete',{note:'本人已阅读并确认'});
    const day=secretaryDay(arrangeSecretaryItems(d),d,'2026-09-06');
    expect(day.completedToday).toHaveLength(1);expect(day.completionReports).toHaveLength(0);
    expect(day.ready).toHaveLength(0);
  });
  it('does not report malformed data as an empty successful day', () => {
    expect(parseWithFallback<SecretaryResponse|null>({projection:{items:'bad'}},SecretaryResponseSchema,null,{endpoint:'test'})).toBeNull();
  });
  it('does not truncate records beyond a page', () => {
    const d=fixture(); const item=d.projection!.items[0]!;
    d.projection!.items=Array.from({length:227},(_,i)=>({...item,key:String(i)}));
    expect(secretaryDay(arrangeSecretaryItems(d),d,'2026-09-06').unplanned).toHaveLength(227);
  });
});
