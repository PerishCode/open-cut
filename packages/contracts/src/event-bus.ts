type EventMap = Record<string, unknown>;
type EventHandler<Value> = (value: Value) => void;

export class EventBus<Events extends EventMap> {
  readonly #handlers = new Map<keyof Events, Set<EventHandler<Events[keyof Events]>>>();

  subscribe<Type extends keyof Events>(type: Type, handler: EventHandler<Events[Type]>): () => void {
    const handlers = this.#handlers.get(type) ?? new Set<EventHandler<Events[keyof Events]>>();
    handlers.add(handler as EventHandler<Events[keyof Events]>);
    this.#handlers.set(type, handlers);
    return () => {
      handlers.delete(handler as EventHandler<Events[keyof Events]>);
      if (handlers.size === 0) this.#handlers.delete(type);
    };
  }

  publish<Type extends keyof Events>(type: Type, value: Events[Type]): void {
    for (const handler of this.#handlers.get(type) ?? []) handler(value);
  }

  clear(): void {
    this.#handlers.clear();
  }
}
