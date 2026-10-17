import { Protocols } from '@/schemas/primitives';

// FORK(mixed-clients): Deployment and cloning share one allowlist, including managed mixed on compatible nodes.
// Sidecar protocols run on the child panel; HTTP, tunnel and tun stay local.
export const NODE_ELIGIBLE_PROTOCOLS: Readonly<Record<string, true>> = {
  // FORK(mixed-clients): Mixed uses the same deploy, edit and clone pipeline as other managed inbounds.
  [Protocols.MIXED]: true,
  [Protocols.VLESS]: true,
  [Protocols.VMESS]: true,
  [Protocols.TROJAN]: true,
  [Protocols.SHADOWSOCKS]: true,
  [Protocols.HYSTERIA]: true,
  [Protocols.WIREGUARD]: true,
  [Protocols.MTPROTO]: true,
  [Protocols.AMNEZIAWG]: true,
  [Protocols.TUIC]: true,
  [Protocols.MASQUE]: true,
};
