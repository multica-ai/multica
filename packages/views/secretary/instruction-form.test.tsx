import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SecretaryItemSchema } from '@multica/core/secretary/contract';
import { InstructionForm } from './instruction-form';
const { save } = vi.hoisted(() => ({save:vi.fn().mockResolvedValue({sequence:1,request_id:'test'})}));
vi.mock('@multica/core/hooks',()=>({useWorkspaceId:()=> 'workspace'}));
vi.mock('@multica/core/api',()=>({api:{saveSecretaryInstruction:save}}));
describe('secretary instruction editing',()=>{
 it('retains the existing waiting date when only the note is edited',async()=>{
  const item=SecretaryItemSchema.parse({key:'a',case_id:'c',title:'八月考勤',situation:'已查到月报',next_step:'等结果',
   kind:'action',stage:'waiting',owner:'external',follow_up_on:'2026-09-08',source_updated_at:'2026-09-06T08:00:00+08:00'});
  render(<QueryClientProvider client={new QueryClient()}><InstructionForm item={item} revision={2} today="2026-09-06" onClose={vi.fn()} /></QueryClientProvider>);
  expect(screen.getByLabelText('处理方式')).toHaveValue('wait');
  expect(screen.getByLabelText('下次跟进日期')).toHaveValue('2026-09-08');
  fireEvent.change(screen.getByLabelText('等谁回复、需要什么结果'),{target:{value:'等负责方确认适用异常'}});
  fireEvent.click(screen.getByRole('button',{name:'保存安排'}));
  await waitFor(()=>expect(save).toHaveBeenCalledWith(expect.objectContaining({kind:'wait',scheduled_on:'2026-09-08',note:'等负责方确认适用异常'})));
 });
});
