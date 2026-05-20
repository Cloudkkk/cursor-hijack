'use client';

interface HeaderTableProps {
  headers: { [key: string]: string[] };
}

export function HeaderTable({ headers }: HeaderTableProps) {
  const entries = Object.entries(headers);

  if (entries.length === 0) {
    return <div className="text-muted-foreground text-sm">No headers</div>;
  }

  return (
    <div className="border rounded overflow-hidden">
      <table className="w-full text-xs">
        <thead className="bg-muted">
          <tr>
            <th className="text-left p-2 font-medium">Header</th>
            <th className="text-left p-2 font-medium">Value</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {entries.map(([key, values]) => (
            <tr key={key} className="hover:bg-muted/50">
              <td className="p-2 font-mono text-purple-600 align-top whitespace-nowrap">
                {key}
              </td>
              <td className="p-2 font-mono break-all">
                {values.map((v, i) => (
                  <div key={i}>{v}</div>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
