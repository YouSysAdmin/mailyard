-- Runs once, on first initialisation of the dev cluster.
--
-- The store tests create and drop their own schemas inside whatever
-- database MAILYARD_TEST_DSN names. Giving them a separate database
-- keeps a bad test run away from the data you are developing against,
-- for the price of one CREATE.
CREATE DATABASE mailyard_test OWNER mailyard;
