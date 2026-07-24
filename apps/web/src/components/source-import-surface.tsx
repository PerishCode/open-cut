import { FileField, Stack, Text } from "@open-cut/components";

export type SourceImportSurfaceProps = Readonly<{
  disabled: boolean;
  error?: Error;
  onSelect(file: File): void;
}>;

export function SourceImportSurface({ disabled, error, onSelect }: SourceImportSurfaceProps) {
  return (
    <Stack spacing="compact">
      <FileField
        accept="video/*,audio/*,.mkv,.m4v,.flac"
        disabled={disabled}
        label="Drop footage here or choose a local file"
        onSelect={onSelect}
      />
      {error ? <Text>Footage could not be added. Choose the file again.</Text> : null}
    </Stack>
  );
}
