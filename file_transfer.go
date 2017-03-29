package stonesthrow

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
)

func zipAndSend(files []string, root_path string, j JobEventSender) error {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, filename := range files {
		relative_path, err := filepath.Rel(root_path, filename)
		if err != nil {
			return err
		}

		file_info, err := os.Lstat(filename)
		if file_info.IsDir() {
			continue
		}

		file_writer, err := writer.Create(relative_path)
		if err != nil {
			return err
		}

		file_reader, err := os.Open(filename)
		if err != nil {
			return err
		}
		_, err = io.Copy(file_writer, file_reader)
		file_reader.Close()
		if err != nil {
			return err
		}
	}

	err := writer.Close()
	if err != nil {
		return err
	}

	return j.Send(&JobEvent{ZippedContent: &ZippedContentEvent{Data: buffer.Bytes()}})
}

func SendFiles(ctx context.Context, workdir string, fetch_options *FetchFileOptions, j JobEventSender) error {
	base_path := filepath.Join(workdir, fetch_options.GetRelativePath())
	if fetch_options.GetFilenameGlob() == "" {
		return zipAndSend([]string{base_path}, workdir, j)
	}

	if fetch_options.GetRecurse() {
		var file_list []string
		p_file_list := &file_list
		err := filepath.Walk(base_path, func(filename string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			matched, err := filepath.Match(fetch_options.FilenameGlob, filepath.Base(filename))
			if err != nil || !matched {
				return nil
			}
			*p_file_list = append(*p_file_list, filename)
			return nil
		})
		if err != nil {
			return err
		}
		return zipAndSend(file_list, workdir, j)
	}

	file_list, err := filepath.Glob(filepath.Join(base_path, fetch_options.FilenameGlob))
	if err != nil {
		return err
	}
	return zipAndSend(file_list, workdir, j)
}

func ReceiveFiles(ctx context.Context, workdir string, no_write bool, zipped_content *ZippedContentEvent, j JobEventSender) error {
	buffer := bytes.NewReader(zipped_content.GetData())
	zip_reader, err := zip.NewReader(buffer, int64(buffer.Len()))
	if err != nil {
		return err
	}

	for _, file := range zip_reader.File {
		current_path := filepath.Join(workdir, file.Name)

		if no_write {
			if file.FileInfo().IsDir() {
				SendLog(j, LogEvent_INFO, "D %s", current_path)
			} else {
				SendLog(j, LogEvent_INFO, ". %s (%d bytes)", current_path, file.UncompressedSize64)
			}
			continue
		}

		if file.FileInfo().IsDir() {
			err = os.MkdirAll(current_path, os.ModeDir|0777)
			if err == nil {
				SendLog(j, LogEvent_ERROR, "In directory: %s", current_path)
			} else {
				SendLog(j, LogEvent_ERROR, "Failed to create path: %s", current_path)
			}
			continue
		}

		_, err := os.Lstat(filepath.Dir(current_path))
		if err != nil && os.IsNotExist(err) {
			err = os.MkdirAll(filepath.Dir(current_path), os.ModeDir|0777)
			if err != nil {
				SendLog(j, LogEvent_ERROR, "Failed to create path: %s", filepath.Dir(current_path))
				continue
			}
		}
		f, err := os.OpenFile(current_path, os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			SendLog(j, LogEvent_ERROR, "Can't open: %s : %s", current_path, err.Error())
			continue
		}

		reader, err := file.Open()
		if err != nil {
			SendLog(j, LogEvent_ERROR, "Can't open stream for %s: %s", current_path, err.Error())
			f.Close()
			continue
		}

		written, err := io.Copy(f, reader)
		if err != nil {
			SendLog(j, LogEvent_ERROR, "Failed to write %s: %s", current_path, err.Error())
		}
		err = f.Close()
		if err != nil {
			SendLog(j, LogEvent_ERROR, "Failed to close %s: %s", current_path, err.Error())
		}
		SendLog(j, LogEvent_INFO, "Wrote %s (%d bytes)", current_path, written)
	}
	return nil
}
