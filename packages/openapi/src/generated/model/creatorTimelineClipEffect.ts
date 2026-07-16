import type { CreatorTimelineClipEffectOutcome } from './creatorTimelineClipEffectOutcome';
import type { CreatorTimelineClipPlacement } from './creatorTimelineClipPlacement';

export interface CreatorTimelineClipEffect {
  after?: CreatorTimelineClipPlacement;
  before: CreatorTimelineClipPlacement;
  clipId: string;
  left?: CreatorTimelineClipPlacement;
  outcome: CreatorTimelineClipEffectOutcome;
  right?: CreatorTimelineClipPlacement;
}
