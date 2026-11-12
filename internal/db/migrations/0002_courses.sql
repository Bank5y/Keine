CREATE TABLE courses (
    id            bigserial PRIMARY KEY,
    uploader_id   bigint      NOT NULL REFERENCES users (id),
    title         text        NOT NULL,
    type          text        NOT NULL CHECK (type IN ('column', 'video', 'paper')),
    completion    text        NOT NULL DEFAULT 'finished' CHECK (completion IN ('finished', 'ongoing', 'incomplete')),
    intro         text        NOT NULL DEFAULT '',
    author_name   text        NOT NULL DEFAULT '',
    author_intro  text        NOT NULL DEFAULT '',
    -- package-relative path of the cover image, if the manifest named one
    cover         text,
    visibility    text        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public')),
    -- where a private course stands towards the library; meaningless once public
    publish_state text        NOT NULL DEFAULT 'unpublished' CHECK (publish_state IN ('unpublished', 'pending', 'rejected')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX courses_uploader_id ON courses (uploader_id);

CREATE TABLE chapters (
    id        bigserial PRIMARY KEY,
    course_id bigint  NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    key       text    NOT NULL,
    title     text    NOT NULL,
    position  integer NOT NULL,
    UNIQUE (course_id, key)
);

CREATE TABLE lessons (
    id         bigserial PRIMARY KEY,
    course_id  bigint  NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    chapter_id bigint  NOT NULL REFERENCES chapters (id) ON DELETE CASCADE,
    key        text    NOT NULL,
    title      text    NOT NULL,
    position   integer NOT NULL,
    -- package-relative paths; video_url is an external link instead of a file
    file       text,
    video_file text,
    video_url  text,
    duration   text    NOT NULL DEFAULT '',
    UNIQUE (course_id, key)
);
CREATE INDEX lessons_chapter_id ON lessons (chapter_id);

-- A user has started learning a course.
CREATE TABLE enrollments (
    user_id         bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_id       bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_studied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, course_id)
);
CREATE INDEX enrollments_course_id ON enrollments (course_id);
