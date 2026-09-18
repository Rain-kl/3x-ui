// Client-side mirror of the backend inbound-tag derivation
// (web/service/port_conflict.go). Keep in sync; inbound-tag.test.ts guards parity.

type TransportBits = number;
const TCP: TransportBits = 1;
const UDP: TransportBits = 2;

function asString(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

function inboundTransports(
  protocol: string,
  streamSettings: Record<string, unknown> | undefined,
  settings: Record<string, unknown> | undefined,
): TransportBits {
  if (
    protocol === 'hysteria' ||
    protocol === 'wireguard' ||
    protocol === 'amneziawg' ||
    protocol === 'tuic'
  )
    return UDP;

  let bits: TransportBits = 0;
  const network = asString(streamSettings?.network);
  if (network === 'kcp' || network === 'quic') bits |= UDP;
  else bits |= TCP;

  if (settings) {
    if (protocol === 'shadowsocks' || protocol === 'tunnel') {
      const key = protocol === 'tunnel' ? 'allowedNetwork' : 'network';
      const n = asString(settings[key]);
      if (n !== '') {
        bits = 0;
        for (const part of n.split(',')) {
          const p = part.trim();
          if (p === 'tcp') bits |= TCP;
          else if (p === 'udp') bits |= UDP;
        }
      }
    } else if (protocol === 'mixed') {
      if (settings.udp === true) bits |= UDP;
    }
  }

  if (bits === 0) bits = TCP;
  return bits;
}

function transportTagSuffix(bits: TransportBits): string {
  if (bits === TCP) return 'tcp';
  if (bits === UDP) return 'udp';
  if (bits === (TCP | UDP)) return 'tcpudp';
  return 'any';
}

function baseInboundTag(port: number): string {
  return `in-${port}`;
}

function nodeTagPrefix(nodeId: number | null | undefined): string {
  return nodeId == null || nodeId <= 0 ? '' : `n${nodeId}-`;
}

/** Remove the central-only n<id>- alias so a tag matches the node's Xray inbound. */
export function stripNodeInboundTagPrefix(nodeId: number | null | undefined, tag: string): string {
  const prefix = nodeTagPrefix(nodeId);
  if (!prefix || !tag.startsWith(prefix)) return tag;
  return tag.slice(prefix.length);
}

export interface InboundTagInput {
  port: number;
  nodeId: number | null | undefined;
  protocol: string;
  streamSettings?: Record<string, unknown>;
  settings?: Record<string, unknown>;
}

export function composeInboundTag(input: InboundTagInput): string {
  const bits = inboundTransports(input.protocol, input.streamSettings, input.settings);
  return (
    nodeTagPrefix(input.nodeId) + baseInboundTag(input.port ?? 0) + '-' + transportTagSuffix(bits)
  );
}

export function isAutoInboundTag(tag: string, input: InboundTagInput): boolean {
  if (tag === '') return true;
  const base = composeInboundTag(input);
  if (tag === base) return true;
  const prefix = `${base}-`;
  if (!tag.startsWith(prefix)) return false;
  const suffix = tag.slice(prefix.length);
  return suffix !== '' && /^[0-9]+$/.test(suffix);
}
