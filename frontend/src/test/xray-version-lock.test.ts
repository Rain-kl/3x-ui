import { describe, expect, it } from 'vitest';

import { Status } from '@/models/status';
import { StatusSchema } from '@/schemas/status';
import { Msg } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

function statusFromServer(versionLock: boolean): Status {
  const msg = new Msg(true, '', {
    xray: {
      state: 'running',
      errorMsg: '',
      version: '26.7.28',
      versionLock,
    },
  });
  const validated = parseMsg(msg, StatusSchema, 'server/status');
  return new Status(validated.obj);
}

describe('xray version lock', () => {
  it('keeps versionLock from /server/status so the Xray update switch can restore it', () => {
    expect(statusFromServer(true).xray.versionLock).toBe(true);
  });

  it('keeps an unlocked versionLock as false instead of dropping the field', () => {
    expect(statusFromServer(false).xray.versionLock).toBe(false);
  });
});
