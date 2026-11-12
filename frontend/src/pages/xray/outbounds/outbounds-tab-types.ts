export interface OutboundRow {
  key: number;
  tag?: string;
  // FORK(outbound-stable-identity): Display names are separate from route identities.
  remark?: string;
  panelId?: string;
  protocol?: string;
  streamSettings?: { network?: string; security?: string };
  settings?: Record<string, unknown>;
}
