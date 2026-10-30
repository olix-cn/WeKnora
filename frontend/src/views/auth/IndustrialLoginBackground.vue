<template>
  <div class="industrial-background" aria-hidden="true">
    <div class="industrial-grid"></div>
    <div class="industrial-rail industrial-rail--top"></div>
    <div class="industrial-rail industrial-rail--bottom"></div>
    <svg class="industrial-drawing" viewBox="0 0 1440 900" preserveAspectRatio="xMidYMid slice" fill="none">
      <g class="drawing-frame">
        <path d="M36 158V112H180M1260 112H1404V158M36 742V788H180M1260 788H1404V742" />
        <path d="M36 182V718M1404 182V718M204 112H1236M204 788H1236" opacity=".35" />
        <path d="M48 176H72M60 164V188M1368 176H1392M1380 164V188M48 724H72M60 712V736M1368 724H1392M1380 712V736" />
      </g>
      <g class="drawing-assembly">
        <path d="M310 135L490 45H900L1080 135V247L900 337H490L310 247Z" />
        <path d="M310 135L490 225H900L1080 135M490 225V337M900 225V337M490 45V157H900V45M490 157L310 247M900 157L1080 247" />
        <path d="M430 105L530 65H850L950 105L850 145H530Z M430 105V139L530 179H850L950 139V105" />
        <path d="M530 145V179M850 145V179M360 214L467 267M924 267L1030 214" />
        <path d="M490 387H900M490 371V403M900 371V403M695 357V417M671 387H719" />
      </g>
      <g class="drawing-base">
        <path d="M180 746L400 636H880L1100 746L880 856H400Z M180 746V778L400 888H880L1100 778V746M400 856V888M880 856V888" />
        <path d="M400 677H880L1018 746L880 815H400L262 746Z M400 677V711H880V677M262 746L400 711M1018 746L880 711" />
        <path d="M477 746H803M640 687V805" stroke-dasharray="5 9" />
        <path d="M400 609V585H880V609M420 597H860M400 597L408 593V601ZM880 597L872 593V601Z" />
      </g>
      <g class="drawing-detail">
        <path d="M1190 152H1350V254H1190ZM1206 168H1334V238H1206ZM1230 184H1310V222H1230Z" />
        <path d="M1218 152V130M1250 152V130M1282 152V130M1314 152V130M1218 254V276M1250 254V276M1282 254V276M1314 254V276" />
        <path d="M1190 190H1168M1190 220H1168M1350 190H1372M1350 220H1372M1158 126V286M1150 126H1166M1150 286H1166" />
      </g>
    </svg>
    <div class="industrial-scan"></div>
    <div class="industrial-fastener industrial-fastener--tl"></div>
    <div class="industrial-fastener industrial-fastener--tr"></div>
    <div class="industrial-fastener industrial-fastener--bl"></div>
    <div class="industrial-fastener industrial-fastener--br"></div>
  </div>
</template>

<style scoped>
/* Hallmark · background study: industrial drafting · AethCraft brand palette
 * pre-emit critique: P4 H4 E4 S5 R5 V4
 */
.industrial-background {
  position: absolute;
  inset: 0;
  overflow: hidden;
  contain: strict;
  pointer-events: none;
  z-index: 0;
  background:
    radial-gradient(ellipse at 18% 48%, var(--login-steel-light), transparent 68%),
    linear-gradient(125deg, var(--login-steel), var(--login-steel-deep));
}

.industrial-grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(var(--login-grid-major) 1px, transparent 1px),
    linear-gradient(90deg, var(--login-grid-major) 1px, transparent 1px),
    linear-gradient(var(--login-grid-minor) 1px, transparent 1px),
    linear-gradient(90deg, var(--login-grid-minor) 1px, transparent 1px);
  background-size: 160px 160px, 160px 160px, 32px 32px, 32px 32px;
  mask-image: linear-gradient(to bottom, var(--login-mask) 0%, transparent 35%, transparent 65%, var(--login-mask) 100%);
}

.industrial-drawing {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  stroke: var(--login-drawing);
  stroke-width: 1;
}

.drawing-frame { opacity: .7; }
.drawing-assembly { opacity: .4; }
.drawing-base { opacity: .32; }
.drawing-detail { opacity: .45; }

.industrial-rail {
  position: absolute;
  left: 36px;
  right: 36px;
  height: 8px;
  border-block: 1px solid var(--login-grid-major);
  background: repeating-linear-gradient(90deg, var(--login-drawing) 0 1px, transparent 1px 16px);
  opacity: .5;
}
.industrial-rail--top { top: 90px; }
.industrial-rail--bottom { bottom: 26px; }

.industrial-scan {
  position: absolute;
  top: 15%;
  bottom: 15%;
  left: 0;
  width: 1px;
  background: linear-gradient(transparent, var(--login-scan), transparent);
  opacity: 0;
  animation: drafting-scan 18s linear infinite;
}

@keyframes drafting-scan {
  0% { transform: translateX(5vw); opacity: 0; }
  8%, 80% { opacity: .28; }
  95%, 100% { transform: translateX(95vw); opacity: 0; }
}

.industrial-fastener {
  position: absolute;
  width: 8px;
  height: 8px;
  border: 1px solid var(--login-drawing);
  border-radius: 50%;
  background: linear-gradient(135deg, transparent 44%, var(--login-drawing) 45% 55%, transparent 56%);
  opacity: .5;
}
.industrial-fastener--tl { top: 26px; left: 20px; }
.industrial-fastener--tr { top: 26px; right: 20px; }
.industrial-fastener--bl { bottom: 26px; left: 20px; }
.industrial-fastener--br { bottom: 26px; right: 20px; }

@media (max-width: 768px) {
  .industrial-drawing { opacity: .55; }
  .drawing-detail { display: none; }
  .industrial-rail { left: 20px; right: 20px; }
  .industrial-rail--top { top: 76px; }
}

@media (max-width: 480px) {
  .industrial-drawing { display: none; }
  .industrial-scan { animation: none; display: none; }
  .industrial-grid { opacity: .6; }
}

@media (prefers-reduced-motion: reduce) {
  .industrial-scan { animation: none; display: none; }
}
</style>
