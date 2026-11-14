export function workMatchesBuilding(code: string, technology: string, building: string) {
  if (code === 'INST_PON_MKD') return technology === 'PON' && building === 'MKD';
  if (code === 'INST_FTTB_MKD') return technology === 'FTTB' && building === 'MKD';
  if (code === 'INST_PRIVATE_VLS' || code === 'SURVEY_PRIVATE') return building === 'PRIVATE';
  return true;
}
