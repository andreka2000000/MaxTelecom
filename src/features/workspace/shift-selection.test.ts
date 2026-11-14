import { describe, expect, it } from 'vitest';
import { chooseShift, ekaterinburgDate, nextShiftDate } from './shift-selection';
import { workMatchesBuilding } from './work-compatibility';
import type { DaySummary } from './types';
const days = ['2026-11-16', '2026-10-30', '2026-10-29'].map((date) => ({ date }) as DaySummary);
describe('operational dates', () => {
  it('preserves a valid selected shift and recovers from a removed one', () => {
    expect(chooseShift(days, '2026-10-29', '2026-11-14')).toBe('2026-10-29');
    expect(chooseShift(days, '2025-01-01', '2026-11-14')).toBe('2026-11-16');
    expect(chooseShift(days, '', '2026-10-30')).toBe('2026-10-30');
    expect(chooseShift([], '', '2026-11-14')).toBe('');
  });
  it('uses the Yekaterinburg calendar and avoids offering an old date', () => {
    expect(ekaterinburgDate(new Date('2026-11-15T20:30:00Z'))).toBe('2026-11-16');
    expect(nextShiftDate('2026-10-30', '2026-11-16')).toBe('2026-11-16');
    expect(nextShiftDate('2026-11-16', '2026-11-16')).toBe('2026-11-17');
  });
});
it('prevents selecting private-sector work for an apartment building and wrong technology', () => {
  expect(workMatchesBuilding('INST_PRIVATE_VLS', 'PON', 'MKD')).toBe(false);
  expect(workMatchesBuilding('SURVEY_PRIVATE', 'PON', 'MKD')).toBe(false);
  expect(workMatchesBuilding('INST_FTTB_MKD', 'PON', 'MKD')).toBe(false);
  expect(workMatchesBuilding('INST_PRIVATE_VLS', 'PON', 'PRIVATE')).toBe(true);
  expect(workMatchesBuilding('REPAIR', 'DOCSIS', 'MKD')).toBe(true);
});
