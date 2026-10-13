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
  it('ignores legacy versionLock while reading the Xray core version', () => {
    for (const versionLock of [true, false]) {
      const status = statusFromServer(versionLock);

      expect(status.xray.version).toBe('26.7.28');
      expect(status.xray).not.toHaveProperty('versionLock');
    }
  });
});
