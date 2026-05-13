"use client";

import * as React from "react";

import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";
import { cn } from "@/lib/utils";

type BasicSelectOption = {
  value: string;
  label: string;
  disabled: boolean;
};

type BasicSelectProps = Omit<
  React.ComponentProps<"select">,
  "children" | "multiple" | "size" | "ref"
> & {
  children: React.ReactNode;
};

function BasicSelect({
  className,
  children,
  value,
  defaultValue,
  disabled = false,
  onChange,
  "aria-label": ariaLabel,
  id,
  name,
  required,
}: BasicSelectProps) {
  const options = React.useMemo(() => flattenOptions(children), [children]);
  const isControlled = value !== undefined;
  const initialValue = React.useMemo(
    () => normalizeSelectValue(isControlled ? value : defaultValue),
    [defaultValue, isControlled, value],
  );
  const [internalValue, setInternalValue] = React.useState(initialValue);
  const selectedValue = isControlled
    ? normalizeSelectValue(value)
    : internalValue;

  React.useEffect(() => {
    if (!isControlled) {
      setInternalValue(initialValue);
    }
  }, [initialValue, isControlled]);

  const selectedOption = React.useMemo(
    () => options.find((option) => option.value === selectedValue) ?? null,
    [options, selectedValue],
  );
  const [open, setOpen] = React.useState(false);
  const [inputValue, setInputValue] = React.useState(selectedOption?.label ?? "");

  React.useEffect(() => {
    setInputValue(selectedOption?.label ?? "");
  }, [selectedOption]);

  const handleValueChange = React.useCallback(
    (nextValue: string) => {
      if (!isControlled) {
        setInternalValue(nextValue);
      }
      onChange?.(
        {
          target: { value: nextValue, name },
          currentTarget: { value: nextValue, name },
        } as React.ChangeEvent<HTMLSelectElement>,
      );
    },
    [isControlled, name, onChange],
  );

  return (
    <>
      <Combobox
        autoHighlight
        items={options}
        itemToStringLabel={(item: BasicSelectOption) => item.label}
        itemToStringValue={(item: BasicSelectOption) =>
          `${item.label} ${item.value}`.trim()
        }
        inputValue={inputValue}
        open={open}
        openOnInputClick
        value={selectedOption}
        onInputValueChange={(nextInputValue) => {
          setInputValue(nextInputValue);
          if (!disabled) {
            setOpen(true);
          }
        }}
        onOpenChange={setOpen}
        onValueChange={(nextValue) => {
          const resolvedValue = nextValue?.value ?? "";
          handleValueChange(resolvedValue);
          setInputValue(nextValue?.label ?? "");
          setOpen(false);
        }}
      >
        <ComboboxInput
          aria-label={ariaLabel}
          id={id}
          className={cn("w-full", className)}
          disabled={disabled}
          onClick={() => {
            if (!disabled) {
              setOpen(true);
            }
          }}
          placeholder={selectedOption?.label ?? ""}
          showTrigger
          readOnly
        />
        <ComboboxContent>
          <ComboboxEmpty>暂无可选项</ComboboxEmpty>
          <ComboboxList>
            {(item: BasicSelectOption) => (
              <ComboboxItem
                disabled={item.disabled}
                key={`${item.value}-${item.label}`}
                value={item}
              >
                {item.label}
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      <input
        disabled={disabled}
        name={name}
        required={required}
        type="hidden"
        value={selectedValue}
      />
    </>
  );
}

function flattenOptions(children: React.ReactNode): BasicSelectOption[] {
  const options: BasicSelectOption[] = [];

  React.Children.forEach(children, (child) => {
    if (!React.isValidElement(child)) {
      return;
    }
    const element = child as React.ReactElement<{ value?: string; disabled?: boolean; children?: React.ReactNode }>;

    if (element.type === React.Fragment) {
      options.push(...flattenOptions(element.props.children));
      return;
    }

    if (typeof element.type === "string" && element.type.toLowerCase() === "optgroup") {
      options.push(...flattenOptions(element.props.children));
      return;
    }

    if (typeof element.type === "string" && element.type.toLowerCase() === "option") {
      options.push({
        value: normalizeSelectValue(element.props.value),
        label: extractOptionLabel(element.props.children),
        disabled: Boolean(element.props.disabled),
      });
    }
  });

  return options;
}

function extractOptionLabel(children: React.ReactNode): string {
  const text = React.Children.toArray(children)
    .map((child) => {
      if (typeof child === "string" || typeof child === "number") {
        return String(child);
      }
      return "";
    })
    .join("")
    .trim();

  return text;
}

function normalizeSelectValue(value: string | number | readonly string[] | undefined): string {
  if (Array.isArray(value)) {
    return value[0] ?? "";
  }
  if (value === undefined || value === null) {
    return "";
  }
  return String(value);
}

export { BasicSelect };
