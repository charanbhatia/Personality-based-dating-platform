import { useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';

export default function VirtualList({
  count,
  estimateSize = 76,
  className = 'virtual-list',
  children,
}) {
  const parentRef = useRef(null);
  // Tanstack Virtual returns unstable function identities; React Compiler skips this component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimateSize,
    overscan: 8,
  });

  return (
    <div ref={parentRef} className={className}>
      <ul
        className="conv-list"
        style={{ height: `${virtualizer.getTotalSize()}px`, position: 'relative', margin: 0 }}
      >
        {virtualizer.getVirtualItems().map((row) => (
          <li
            key={row.key}
            data-index={row.index}
            ref={virtualizer.measureElement}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              transform: `translateY(${row.start}px)`,
            }}
          >
            {children(row.index)}
          </li>
        ))}
      </ul>
    </div>
  );
}
