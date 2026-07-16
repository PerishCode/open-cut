import type { SourceStreamDescriptor } from './sourceStreamDescriptor';

export interface SourceStream {
  descriptor: SourceStreamDescriptor;
  id: string;
}
