if __name__ != '__main__':
    raise EnvironmentError('host.py cannot be imported.')

import os
import sys
import argparse
import importlib
import json 

from stonesthrow import Options

def ConfigureCommonArgs(parser):
    # Add common flags.
    m = parser.add_mutually_exclusive_group()
    m.add_argument('--config', type=str, action='store', metavar='CONFIG', help='Configuration. should be specified as a JSON string.')
    m.add_argument('--config_file', nargs='?', type=argparse.FileType('r'), metavar='CONFIG_FILE', help='configuration file should contain valid JSON.')
    
    parser.add_argument('--module', type=str, action='store', metavar='MODULE_NAME', help='module name to load.')
    parser.add_argument('--sys_path', type=str, action='append', metavar='PATH', help='path to append to sys.path. Can be specified more than once.')
    parser.add_argument('args', metavar='ARGS', nargs=argparse.REMAINDER)

def LoadModule(options):
    if options.sys_path:
        for p in options.sys_path:
            sys.path.append(p)

    if not options.module:
        raise ValueError('--module must be specified')

    return importlib.import_module(options.module)

def ParseConfig(options):
    if not options.config and not options.config_file:
        raise ValueError('Either "config" or "config_file" must be specified')

    if options.config_file:
        return json.load(options.config_file, encoding='utf-8')
    return json.loads(options.config, encoding='utf-8')


def Main(args):
    parser = argparse.ArgumentParser(description='Python script host for Stonesthrow', add_help=False)
    ConfigureCommonArgs(parser)

    options = parser.parse_args(args, namespace=Options())
    if not options:
        return

    module = LoadModule(options)
    config = ParseConfig(options)

    child_parser = module.ConfigureFlags(options)
    child_options = child_parser.parse_args(options.args, namespace=Options())
    child_options.__dict__.update(config)

    module.Run(child_options)

Main(sys.argv[1:])

