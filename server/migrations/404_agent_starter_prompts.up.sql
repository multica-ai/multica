ALTER TABLE agent
ADD COLUMN starter_prompts JSONB NOT NULL DEFAULT '[]'::jsonb
CHECK (
    jsonb_typeof(starter_prompts) = 'array'
    AND jsonb_array_length(starter_prompts) <= 3
);
