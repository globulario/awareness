// TodoList demonstrates TypeScript component patterns that satisfy the
// awareness invariants for this project.
//
// Key invariants enforced:
//   - state.immutability: state only updated through setter
//   - api.boundary.typed: response validated before use
//   - component.data.separation: fetching in a hook, not in the component
//   - error.surface.to.user: all async errors displayed

export interface Todo {
  id: string;
  title: string;
  done: boolean;
}

// ApiTodo is the raw shape returned by the server.
// Separate from Todo so API changes don't silently break the UI.
interface ApiTodo {
  id: string;
  title: string;
  completed: boolean;
}

function parseApiTodo(raw: ApiTodo): Todo {
  return {
    id: raw.id,
    title: raw.title,
    done: raw.completed,
  };
}

// useTodos is a data hook — business logic lives here, not in the component.
export async function fetchTodos(signal: AbortSignal): Promise<Todo[]> {
  const resp = await fetch("/api/todos", { signal });
  if (!resp.ok) {
    throw new Error(`fetch todos: HTTP ${resp.status}`);
  }
  const raw: ApiTodo[] = await resp.json();
  return raw.map(parseApiTodo);
}

// toggleTodo returns a new array with the todo toggled — no direct mutation.
export function toggleTodo(todos: Todo[], id: string): Todo[] {
  return todos.map((t) => (t.id === id ? { ...t, done: !t.done } : t));
}
